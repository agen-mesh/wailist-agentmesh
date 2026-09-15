package nodes

import (
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func TestIsDegradable(t *testing.T) {
	tests := []struct {
		name string
		node models.WorkflowNode
		want bool
	}{
		{"http GET", models.WorkflowNode{Type: models.NodeTypeTool, Template: "http", Method: "GET"}, true},
		{"http blank method defaults to GET", models.WorkflowNode{Type: models.NodeTypeTool, Template: "http"}, true},
		{"http lowercase get", models.WorkflowNode{Type: models.NodeTypeTool, Template: "http", Method: "get"}, true},
		{"http POST", models.WorkflowNode{Type: models.NodeTypeTool, Template: "http", Method: "POST"}, false},
		{"http DELETE", models.WorkflowNode{Type: models.NodeTypeTool, Template: "http", Method: "DELETE"}, false},
		{"websearch", models.WorkflowNode{Type: models.NodeTypeTool, Template: "websearch"}, true},
		{"json_extract", models.WorkflowNode{Type: models.NodeTypeTool, Template: "json_extract"}, true},
		{"coingecko", models.WorkflowNode{Type: models.NodeTypeAction, Template: "coingecko"}, true},
		{"rss", models.WorkflowNode{Type: models.NodeTypeAction, Template: "rss"}, true},
		{"slack send", models.WorkflowNode{Type: models.NodeTypeAction, Template: "slack"}, false},
		{"postgres write", models.WorkflowNode{Type: models.NodeTypeAction, Template: "db"}, false},
		{"graphql can mutate", models.WorkflowNode{Type: models.NodeTypeAction, Template: "graphql"}, false},
		{"gmail_list", models.WorkflowNode{Type: models.NodeTypeGoogle, Template: "gmail_list"}, true},
		{"gmail_send", models.WorkflowNode{Type: models.NodeTypeGoogle, Template: "gmail_send"}, false},
		{"state get", models.WorkflowNode{Type: models.NodeTypeState, Template: "get"}, true},
		{"state set", models.WorkflowNode{Type: models.NodeTypeState, Template: "set"}, false},
		{"agent", models.WorkflowNode{Type: models.NodeTypeAgent, Template: "agent"}, false},
		{"tendril rent", models.WorkflowNode{Type: models.NodeTypeTendril, Template: "tendril_rent"}, false},
		// tool402 and walletpay are not catalog templates at all. The
		// fail-closed default must cover them: they move real money.
		{"tool402 is not in the catalog", models.WorkflowNode{Type: models.NodeTypeTool402, Template: ""}, false},
		{"unknown template", models.WorkflowNode{Type: models.NodeTypeAction, Template: "does_not_exist"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDegradable(tt.node); got != tt.want {
				t.Fatalf("IsDegradable(%s/%s) = %v, want %v", tt.node.Type, tt.node.Template, got, tt.want)
			}
		})
	}
}

// TestEveryTemplateIsClassified is the test that catches a connector added
// later without a kind. An unclassified template still behaves safely (the
// default is "action"), but silently: this makes the omission loud.
func TestEveryTemplateIsClassified(t *testing.T) {
	for _, ty := range NodeCatalogData().Types {
		for _, tpl := range ty.Templates {
			switch tpl.Kind {
			case "read", "action":
			default:
				t.Errorf("template %s/%s has kind %q, want \"read\" or \"action\" -- set it in frontend/src/lib/nodeCatalog.ts and run `npm run gen:node-catalog`", ty.Type, tpl.ID, tpl.Kind)
			}
		}
	}
}
