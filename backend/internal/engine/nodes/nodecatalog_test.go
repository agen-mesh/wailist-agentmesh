package nodes

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

// nodecatalog.json is generated from the frontend (see
// frontend/src/lib/nodeCatalog.ts). The frontend's sync test keeps the file
// in step with the canvas; these keep it in step with the ENGINE -- a
// template the engine cannot dispatch, or a setting it never reads, is a
// promise the builder would make that no run can keep.

// builderFiles name keys without ever reading them (allowlists, prompts), so
// they are excluded from sourceOf -- counting them would let a key "pass"
// just because the builder mentions it.
var builderFiles = map[string]bool{
	"graphbuilder.go": true, "graphvalidate.go": true, "nodecatalog.go": true, "graphx402.go": true,
	"graphfetch.go": true, "graphsteps.go": true, "dryrun.go": true,
}

// sourceOf concatenates every non-test, non-builder .go file in this package:
// the code that actually executes nodes.
func sourceOf(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || builderFiles[n] {
			continue
		}
		data, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// caseLabels returns every string literal used as a case label in file.
func caseLabels(t *testing.T, file string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, line := range regexp.MustCompile(`(?m)^\s*case ("[^"]*"(?:,\s*"[^"]*")*):`).FindAllStringSubmatch(string(data), -1) {
		for _, lit := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(line[1], -1) {
			out[lit[1]] = true
		}
	}
	return out
}

func TestNodeCatalogLoads(t *testing.T) {
	c := NodeCatalogData()
	if c.Version != 1 || len(c.Types) == 0 {
		t.Fatalf("catalog did not load: %+v", c)
	}
	for _, typ := range c.Types {
		if _, ok := graphNodeTypes[typ.Type]; !ok {
			t.Errorf("catalog type %q is not a type the builder may create", typ.Type)
		}
	}
}

func TestNodeCatalogTemplatesAreDispatchedByTheEngine(t *testing.T) {
	dispatch := map[string]map[string]bool{
		"action": caseLabels(t, "action.go"),
		"tool":   caseLabels(t, "tool.go"),
		"google": caseLabels(t, "google.go"),
	}
	for _, typ := range NodeCatalogData().Types {
		labels, ok := dispatch[typ.Type]
		if !ok {
			continue
		}
		for _, tpl := range typ.Templates {
			if !labels[tpl.ID] {
				t.Errorf("%s/%s is in the catalog but the engine has no case for it", typ.Type, tpl.ID)
			}
		}
	}
}

// State and Tendril run on a preset field, not the template, so the preset
// value is what has to match the engine.
func TestNodeCatalogPresetsMatchTheEngine(t *testing.T) {
	state := caseLabels(t, "state.go")
	tendril := caseLabels(t, "tendril.go")
	for _, typ := range NodeCatalogData().Types {
		for _, tpl := range typ.Templates {
			switch typ.Type {
			case "state":
				if op := tpl.Presets["stateOp"]; !state[op] {
					t.Errorf("state/%s presets stateOp %q, which state.go does not handle", tpl.ID, op)
				}
			case "tendril":
				if a := tpl.Presets["tendrilAction"]; !tendril[a] {
					t.Errorf("tendril/%s presets tendrilAction %q, which tendril.go does not handle", tpl.ID, a)
				}
			}
		}
	}
}

// Every config and secret key must actually be read somewhere in the engine.
// An Inspector field the engine ignores is harmless for a human; for the
// builder it is a setting it will confidently fill in to no effect.
func TestNodeCatalogKeysAreReadByTheEngine(t *testing.T) {
	src := sourceOf(t)
	// A few credentials are top-level encrypted WorkflowNode properties
	// (apiKey, emailApiKey) rather than Secrets-map keys, so the engine reads
	// them as node.APIKey, never as the string "apiKey". Map json tag -> Go
	// field name so those are checked as the property access they really are.
	goField := map[string]string{}
	rt := reflect.TypeOf(models.WorkflowNode{})
	for i := 0; i < rt.NumField(); i++ {
		goField[strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]] = rt.Field(i).Name
	}
	var missing []string
	for _, typ := range NodeCatalogData().Types {
		for _, tpl := range typ.Templates {
			for _, f := range tpl.Fields {
				if f.Where != "config" && f.Where != "secret" && f.Where != "connection" {
					continue
				}
				if f.Key == "messageTemplate" && strings.Contains(src, `messageTemplateKey = "messageTemplate"`) {
					continue
				}
				if name, ok := goField[f.Key]; ok {
					if !strings.Contains(src, "."+name) {
						missing = append(missing, typ.Type+"/"+tpl.ID+":"+f.Key+" (node."+name+")")
					}
					continue
				}
				if !strings.Contains(src, `"`+f.Key+`"`) {
					missing = append(missing, typ.Type+"/"+tpl.ID+":"+f.Key)
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("catalog keys the engine never reads:\n  %s", strings.Join(missing, "\n  "))
	}
}

// Every "field" key must name a real top-level string property on
// WorkflowNode, since that is where the builder will write it.
func TestNodeCatalogFieldKeysExistOnWorkflowNode(t *testing.T) {
	known := map[string]bool{}
	rt := reflect.TypeOf(models.WorkflowNode{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() != reflect.String {
			continue
		}
		known[strings.Split(f.Tag.Get("json"), ",")[0]] = true
	}
	for _, typ := range NodeCatalogData().Types {
		for _, tpl := range typ.Templates {
			for _, f := range tpl.Fields {
				if f.Where == "field" && !known[f.Key] {
					t.Errorf("%s/%s field %q is not a string property of WorkflowNode", typ.Type, tpl.ID, f.Key)
				}
			}
			for k := range tpl.Presets {
				if !known[k] {
					t.Errorf("%s/%s preset %q is not a string property of WorkflowNode", typ.Type, tpl.ID, k)
				}
			}
		}
	}
}
