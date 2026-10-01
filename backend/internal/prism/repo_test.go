package prism

import (
	"strings"
	"testing"
)

func TestParseRepoURLAcceptsWhatPeopleActuallyPaste(t *testing.T) {
	cases := []struct {
		in          string
		owner, name string
		ref         string
	}{
		{"https://github.com/octocat/Hello-World", "octocat", "Hello-World", ""},
		{"github.com/octocat/Hello-World", "octocat", "Hello-World", ""},
		{"https://www.github.com/octocat/Hello-World/", "octocat", "Hello-World", ""},
		{"https://github.com/octocat/Hello-World.git", "octocat", "Hello-World", ""},
		{"octocat/Hello-World", "octocat", "Hello-World", ""},
		// A link copied from a branch or a file view names its ref.
		{"https://github.com/octocat/Hello-World/tree/develop", "octocat", "Hello-World", "develop"},
		{"https://github.com/octocat/Hello-World/blob/main/src/index.ts", "octocat", "Hello-World", "main"},
		{"  https://github.com/octocat/Hello-World  ", "octocat", "Hello-World", ""},
	}
	for _, c := range cases {
		got, err := ParseRepoURL(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got.Owner != c.owner || got.Name != c.name || got.Ref != c.ref {
			t.Errorf("%q -> %+v, want %s/%s@%q", c.in, got, c.owner, c.name, c.ref)
		}
	}
}

// The error messages here are read by a user who pasted the wrong thing, so
// each one has to say what is wrong rather than just refusing.
func TestParseRepoURLRejectsWhatItCannotHandle(t *testing.T) {
	for _, bad := range []string{
		"",
		"   ",
		"https://gitlab.com/owner/repo",
		"https://bitbucket.org/owner/repo",
		"https://github.com/octocat",
		"https://github.com/",
		"not a url at all",
	} {
		if _, err := ParseRepoURL(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestClassifyFileSkipsWhatWouldWasteMoney is the test that matters
// financially: every file this lets through costs $0.10 and a slot in the run.
func TestClassifyFileSkipsWhatWouldWasteMoney(t *testing.T) {
	skipped := []struct {
		path string
		size int64
	}{
		{"node_modules/react/index.js", 1000},
		{"frontend/node_modules/lodash/get.js", 1000},
		{"vendor/github.com/x/y/z.go", 1000},
		{"dist/bundle.js", 1000},
		{"frontend/.next/static/chunk.js", 1000},
		{"package-lock.json", 500000},
		{"go.sum", 90000},
		{"yarn.lock", 90000},
		{"static/app.min.js", 1000},
		{"README.md", 1000},         // prose, not code
		{"logo.png", 5000},          // binary
		{"Makefile", 500},           // no extension
		{"src/huge.go", 900 * 1024}, // blows the model's context
		{"src/empty.ts", 0},         // nothing to review
	}
	for _, c := range skipped {
		got := ClassifyFile(c.path, c.size)
		if got.Skip == "" {
			t.Errorf("%s (%d bytes) would be sent and paid for; it should be skipped", c.path, c.size)
		}
	}

	kept := []string{
		"src/index.ts", "main.go", "app/models/user.rb", "lib/parser.rs",
		"cmd/server/main.go", "components/Button.tsx", "scripts/deploy.sh",
	}
	for _, p := range kept {
		if got := ClassifyFile(p, 2048); got.Skip != "" {
			t.Errorf("%s was skipped as %q; it is reviewable source", p, got.Skip)
		}
	}
}

// A path that merely CONTAINS a skip-directory name is not in that directory:
// "src/distribution/parser.go" is the author's own code.
func TestClassifyFileDoesNotOverMatchDirectoryNames(t *testing.T) {
	for _, p := range []string{
		"src/distribution/parser.go",
		"internal/building/plan.go",
		"app/outbox/send.go",
	} {
		if got := ClassifyFile(p, 2048); got.Skip != "" {
			t.Errorf("%s was skipped as %q — the directory name only matched as a substring", p, got.Skip)
		}
	}
}

func TestReviewableFiltersAndRawURLIsRaw(t *testing.T) {
	files := []RepoFile{
		{Path: "a.go"},
		{Path: "package-lock.json", Skip: "lockfile"},
		{Path: "b.ts"},
	}
	if got := Reviewable(files); len(got) != 2 {
		t.Fatalf("want 2 reviewable, got %d", len(got))
	}

	r := RepoRef{Owner: "octocat", Name: "Hello-World", Ref: "main"}
	want := "https://raw.githubusercontent.com/octocat/Hello-World/main/src/index.ts"
	// PRISM fetches this itself, so it has to be raw text — a github.com/blob
	// URL returns an HTML page and the review would be of the page chrome.
	if got := r.RawURL("src/index.ts"); got != want {
		t.Errorf("RawURL = %q, want %q", got, want)
	}
}

// TestClassifyFileRejectsPathsOutsideTheRepo is a security guard, not a
// filtering nicety. The file list the browser renders came from us, but the
// paths it posts back are caller-controlled, and a traversal with a reviewable
// extension (".go", ".ts") would otherwise build a raw URL that climbs out of
// the repository.
func TestClassifyFileRejectsPathsOutsideTheRepo(t *testing.T) {
	for _, p := range []string{
		"../../src/main.go",
		"../secrets.ts",
		"src/../../../etc/hosts.go",
		"/etc/passwd.go",
		"https://evil.example.com/x.go",
		"..",
		"src/..",
		"",
	} {
		got := ClassifyFile(p, 2048)
		if got.Skip == "" {
			t.Errorf("%q was accepted as reviewable — it escapes the repository", p)
		}
	}

	// Ordinary relative paths, including ones with dots in the filename, stay
	// reviewable: the guard must not over-reach.
	for _, p := range []string{
		"src/main.go", "a/b/c/deep.ts", "src/file.test.ts", "src/.hidden.go",
	} {
		if got := ClassifyFile(p, 2048); got.Skip != "" {
			t.Errorf("%q was skipped as %q; it is an ordinary repo-relative path", p, got.Skip)
		}
	}
}

// TestRawURLEscapesPathsThatWouldTruncate covers characters that are legal in a
// git path but structural in a URL. Unescaped, "src/a#b.go" becomes
// ".../src/a" — Prism fetches the wrong file, or none, and the call is still
// paid for.
func TestRawURLEscapesPathsThatWouldTruncate(t *testing.T) {
	r := RepoRef{Owner: "octocat", Name: "Hello-World", Ref: "main"}
	for _, c := range []struct{ in, want string }{
		{"src/a#b.go", "https://raw.githubusercontent.com/octocat/Hello-World/main/src/a%23b.go"},
		{"src/a?b.go", "https://raw.githubusercontent.com/octocat/Hello-World/main/src/a%3Fb.go"},
		{"src/my file.go", "https://raw.githubusercontent.com/octocat/Hello-World/main/src/my%20file.go"},
	} {
		if got := r.RawURL(c.in); got != c.want {
			t.Errorf("RawURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// Ordinary paths must come through byte-identical: over-escaping the "/"
	// separators would break every normal file.
	want := "https://raw.githubusercontent.com/octocat/Hello-World/main/src/index.ts"
	if got := r.RawURL("src/index.ts"); got != want {
		t.Errorf("RawURL = %q, want %q", got, want)
	}
	// A ref with a slash (release/1.0) is a real branch name shape.
	br := RepoRef{Owner: "o", Name: "n", Ref: "release/1.0"}
	if got := br.RawURL("main.go"); !strings.Contains(got, "release%2F1.0") {
		t.Errorf("a slashed ref was not escaped: %q", got)
	}
}

// A scheme-less profile URL has the same shape as the owner/name shorthand,
// and taking it literally produced "no public repository at
// github.com/github.com/octocat". The URL branch rejects the same link with a
// scheme, so the two spellings have to agree.
func TestParseRepoURLRejectsASchemelessProfileURL(t *testing.T) {
	for _, bad := range []string{
		"github.com/octocat",
		"GitHub.com/octocat",
		"www.github.com/octocat",
	} {
		if got, err := ParseRepoURL(bad); err == nil {
			t.Errorf("%q parsed as %s/%s; it names a user, not a repository", bad, got.Owner, got.Name)
		}
	}
	// The genuine shorthand still works.
	if got, err := ParseRepoURL("octocat/Hello-World"); err != nil || got.Owner != "octocat" {
		t.Errorf("octocat/Hello-World broke: %+v %v", got, err)
	}
}
