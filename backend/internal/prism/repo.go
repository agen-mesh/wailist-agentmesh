package prism

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Repo review: turning a repository link into a list of files worth paying to
// review.
//
// PRISM's code-review endpoints take one file per call, so a repo review is N
// calls. Two consequences shape everything here:
//
//  1. Every file in the list costs real money. Sending a 2 MB lockfile, a
//     minified bundle or a PNG to an LLM code reviewer wastes the call AND
//     produces nothing useful, so filtering is not a nicety — it is most of
//     the feature's value.
//  2. The user has to see the list, and the bill, before anything is charged.
//     That is why parsing/filtering lives here as pure functions: the handler
//     can show a costed list and only pay once the user has picked.

// RepoRef identifies a GitHub repository and the ref to read.
type RepoRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Ref   string `json:"ref"`
}

// String renders "owner/name@ref" for logs and UI.
func (r RepoRef) String() string { return r.Owner + "/" + r.Name + "@" + r.Ref }

// RawURL is the raw.githubusercontent.com URL PRISM fetches a file from.
// PRISM opens the file itself, so this has to be the raw text, publicly
// reachable, with no redirect through GitHub's HTML view.
func (r RepoRef) RawURL(filePath string) string {
	// Each segment is escaped separately: '#', '?' and spaces are all legal in
	// a git path, and pasted raw they silently truncate the URL at the fragment
	// or turn the rest of the path into a query string. Prism then fetches the
	// wrong thing — or nothing — and the call is still paid for.
	// PathEscape rather than escaping the whole string, so the '/' separators
	// survive.
	escaped := make([]string, 0, 8)
	for _, seg := range strings.Split(filePath, "/") {
		escaped = append(escaped, url.PathEscape(seg))
	}
	return "https://raw.githubusercontent.com/" +
		url.PathEscape(r.Owner) + "/" +
		url.PathEscape(r.Name) + "/" +
		url.PathEscape(r.Ref) + "/" +
		strings.Join(escaped, "/")
}

// ParseRepoURL accepts the forms a user actually pastes and normalises them.
//
// Deliberately GitHub-only for now: PRISM needs an unauthenticated raw-text URL
// per file, and every host spells that differently. Guessing a raw URL scheme
// for a host we have not tested would produce a list of files that all fail
// AFTER being paid for.
func ParseRepoURL(raw string) (RepoRef, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return RepoRef{}, fmt.Errorf("paste a GitHub repository link")
	}
	// Accept "owner/name" shorthand.
	//
	// "github.com/octocat" also matches that shape, and taking it literally
	// yields owner "github.com" and the useless error "no public repository at
	// github.com/github.com/octocat". The URL branch below already rejects the
	// same link with its scheme attached, so without this the two spellings
	// disagree about the same input.
	if !strings.Contains(s, "://") && strings.Count(s, "/") == 1 && !strings.Contains(s, " ") {
		parts := strings.Split(s, "/")
		host := strings.ToLower(parts[0])
		isHost := host == "github.com" || host == "www.github.com"
		if parts[0] != "" && parts[1] != "" && !isHost {
			return RepoRef{Owner: parts[0], Name: strings.TrimSuffix(parts[1], ".git"), Ref: ""}, nil
		}
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return RepoRef{}, fmt.Errorf("that does not look like a link")
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	if host != "github.com" {
		return RepoRef{}, fmt.Errorf("only GitHub repositories are supported right now, not %s", host)
	}
	seg := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(seg) < 2 || seg[0] == "" || seg[1] == "" {
		return RepoRef{}, fmt.Errorf("that link has no repository in it — it should look like github.com/owner/repo")
	}
	ref := ""
	// .../tree/<ref>/... and .../blob/<ref>/... both name a ref.
	if len(seg) >= 4 && (seg[2] == "tree" || seg[2] == "blob") {
		ref = seg[3]
	}
	return RepoRef{Owner: seg[0], Name: strings.TrimSuffix(seg[1], ".git"), Ref: ref}, nil
}

// RepoFile is one candidate file, costed and either included or explained away.
type RepoFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Skip is empty when the file is reviewable. Otherwise it is a short,
	// user-facing reason ("lockfile", "build output") — shown rather than
	// hidden, so nobody wonders why their file is missing from the bill.
	Skip string `json:"skip,omitempty"`
}

// maxReviewableBytes bounds a single file. PRISM fetches the file itself, and a
// very large one blows the model's context and returns a truncated review that
// still costs full price.
const maxReviewableBytes = 256 * 1024

// reviewableExts is an allowlist, not a blocklist. A repo contains far more
// kinds of file than anyone can enumerate, and the failure modes differ: an
// unknown extension that we skip costs the user nothing, while an unknown
// extension we send costs $0.10 and returns a review of a binary blob.
var reviewableExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".py": true, ".rb": true, ".java": true, ".kt": true, ".swift": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true, ".cs": true,
	".rs": true, ".php": true, ".scala": true, ".sh": true, ".bash": true,
	".sql": true, ".vue": true, ".svelte": true, ".dart": true, ".ex": true,
	".exs": true, ".erl": true, ".lua": true, ".pl": true, ".r": true, ".m": true,
}

// skipDirs are paths whose contents are never the author's own code.
var skipDirs = []string{
	"node_modules/", "vendor/", "dist/", "build/", "out/", ".next/", "target/",
	"third_party/", "thirdparty/", "generated/", ".git/", "coverage/",
	"__pycache__/", ".venv/", "venv/", "site-packages/",
}

// lockfiles are machine-written and enormous. Reviewing one is pure waste.
var lockfiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"go.sum": true, "Cargo.lock": true, "poetry.lock": true, "Gemfile.lock": true,
	"composer.lock": true,
}

// ClassifyFile decides whether one path is worth paying to review, and says why
// when it is not. Pure, so the whole list can be costed before any money moves.
func ClassifyFile(filePath string, size int64) RepoFile {
	f := RepoFile{Path: filePath, Size: size}

	// Reject anything that is not a plain, repo-relative path BEFORE looking at
	// the extension. "../../src/main.go" has a reviewable extension and would
	// otherwise pass, producing a raw URL that climbs out of the repository —
	// the caller controls this string, and the list the browser was shown is
	// not a constraint on what it can post back.
	if filePath == "" ||
		strings.HasPrefix(filePath, "/") ||
		strings.Contains(filePath, "\\") ||
		strings.Contains(filePath, "://") ||
		filePath == ".." ||
		strings.HasPrefix(filePath, "../") ||
		strings.Contains(filePath, "/../") ||
		strings.HasSuffix(filePath, "/..") {
		f.Skip = "not a path inside this repository"
		return f
	}

	lower := strings.ToLower(filePath)

	for _, d := range skipDirs {
		if strings.HasPrefix(lower, d) || strings.Contains(lower, "/"+d) {
			f.Skip = "dependency or build output"
			return f
		}
	}
	base := path.Base(filePath)
	if lockfiles[base] {
		f.Skip = "lockfile"
		return f
	}
	// .min.js / .min.css and friends: machine-generated, one enormous line.
	if strings.Contains(lower, ".min.") {
		f.Skip = "minified"
		return f
	}
	ext := strings.ToLower(path.Ext(filePath))
	if !reviewableExts[ext] {
		if ext == "" {
			f.Skip = "not a source file"
		} else {
			f.Skip = "not a reviewable source type"
		}
		return f
	}
	if size > maxReviewableBytes {
		f.Skip = "too large to review in one call"
		return f
	}
	if size == 0 {
		f.Skip = "empty"
		return f
	}
	return f
}

// Reviewable returns just the files that would actually be sent.
func Reviewable(files []RepoFile) []RepoFile {
	out := make([]RepoFile, 0, len(files))
	for _, f := range files {
		if f.Skip == "" {
			out = append(out, f)
		}
	}
	return out
}
