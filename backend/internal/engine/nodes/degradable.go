package nodes

import (
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// httpReadMethods are the HTTP methods that only fetch. Written as an
// allowlist rather than as the negation of httpMethodsWithBody (tool.go)
// because the method token is extensible by design (RFC 7231 §4.1): WebDAV
// alone adds MKCOL, COPY and PROPPATCH, all of which write, and callHTTP
// passes node.Method straight through with no validation, so a typo like
// "PSOT" reaches here too. Negating the body list would read every one of
// them as a read.
var httpReadMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// IsDegradable reports whether this node's failure may be turned into an
// error payload the run continues with, instead of failing the run.
//
// Only a node that fetches from outside the workflow qualifies. That source
// can be down now and answering again on the next run, which is what makes
// carrying on with "this source failed" the honest answer. A template that
// runs in-process is classified "compute" and never degrades: the same input
// produces the same failure forever, so degrading it would turn an authoring
// bug into a permanently partial answer nobody is told to fix.
//
// Fail closed at every step: an unrecognised method, a node whose template is
// not in the catalog at all (tool402, walletpay -- both of which move real
// money) and a template with no kind all answer false. The same stance
// engine.Runner's retry loop already takes on retryability: what this code
// does not understand, it does not degrade.
func IsDegradable(n models.WorkflowNode) bool {
	// An http tool is classified by what it does, not by its template: the
	// same template GETs or POSTs depending on its config, so the catalog
	// cannot answer for it.
	if n.Type == models.NodeTypeTool && n.Template == "http" {
		method := strings.ToUpper(strings.TrimSpace(n.Method))
		if method == "" {
			method = http.MethodGet // callHTTP's own default
		}
		return httpReadMethods[method]
	}
	tpl, ok := catalogTemplate(string(n.Type), n.Template)
	if !ok {
		return false
	}
	return tpl.Kind == "read"
}
