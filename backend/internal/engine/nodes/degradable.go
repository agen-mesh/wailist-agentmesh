package nodes

import (
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// IsDegradable reports whether this node's failure may be turned into an
// error payload the run continues with, instead of failing the run.
//
// Fail closed twice over: a node whose template is not in the catalog at all
// (tool402, walletpay -- both of which move real money) and a template with
// no kind both answer false. The same stance engine.Runner's retry loop
// already takes on retryability: what this code does not understand, it does
// not degrade.
func IsDegradable(n models.WorkflowNode) bool {
	// An http tool is classified by what it does, not by its template: a GET
	// reads, and anything else may write. httpMethodsWithBody (tool.go) is
	// the same line drawn for the same reason, so read it rather than
	// restating the method list here.
	if n.Type == models.NodeTypeTool && n.Template == "http" {
		method := strings.ToUpper(strings.TrimSpace(n.Method))
		if method == "" {
			method = http.MethodGet // callHTTP's own default
		}
		return !httpMethodsWithBody[method]
	}
	tpl, ok := catalogTemplate(string(n.Type), n.Template)
	if !ok {
		return false
	}
	return tpl.Kind == "read"
}
