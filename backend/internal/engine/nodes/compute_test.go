package nodes_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestSetToolBuildsObjectFromTemplates(t *testing.T) {
	rc := engine.NewRunContext("r1", []byte(`"what is the weather"`))
	rc.Set("n1", map[string]any{"city": "Kolkata", "temp": 31.5})

	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool, Template: "set",
		Config: map[string]string{
			"setFields": `{"place":"{{ node.n1.city }}","reading":"{{ node.n1.temp }}C","asked":"{{ input }}"}`,
		},
	}
	out, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("want map output so downstream {{ node.s1.field }} works, got %T", out)
	}
	if got["place"] != "Kolkata" {
		t.Errorf("place: got %v", got["place"])
	}
	if got["reading"] != "31.5C" {
		t.Errorf("reading: got %v", got["reading"])
	}
	if got["asked"] != "what is the weather" {
		t.Errorf("asked: got %v", got["asked"])
	}
}

func TestSetToolErrorsOnInvalidJSON(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "s1", Type: models.NodeTypeTool, Template: "set",
		Config: map[string]string{"setFields": `{"a": }`},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for malformed setFields, got nil")
	}
}

func TestSetToolErrorsWhenUnconfigured(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{ID: "s1", Type: models.NodeTypeTool, Template: "set"}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error when setFields is unset, got nil")
	}
}

func TestJSONExtractPullsNestedValue(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `{"data":{"items":[{"name":"first"},{"name":"second"}]},"ok":true}`)

	cases := []struct {
		name, path string
		want       any
	}{
		{"nested object", "data.items.0.name", "first"},
		{"array index", "data.items.1.name", "second"},
		{"bool at root", "ok", true},
		{"whole subtree", "data", map[string]any{"items": []any{
			map[string]any{"name": "first"}, map[string]any{"name": "second"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := models.WorkflowNode{
				ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
				Config: map[string]string{"jsonPath": tc.path},
			}
			got, err := nodes.ExecuteTool(context.Background(), node, rc)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "whole subtree" {
				m, ok := got.(map[string]any)
				if !ok || len(m) != 1 {
					t.Fatalf("want the data subtree, got %#v", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestJSONExtractErrorsOnMissingPath(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", `{"a":1}`)
	node := models.WorkflowNode{
		ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
		Config: map[string]string{"jsonPath": "a.b.c"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for a path that does not exist, got nil")
	}
}

func TestJSONExtractErrorsOnNonJSONInput(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "this is not json")
	node := models.WorkflowNode{
		ID: "j1", Type: models.NodeTypeTool, Template: "json_extract",
		Config: map[string]string{"jsonPath": "a"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for non-JSON input, got nil")
	}
}

func TestCryptoActions(t *testing.T) {
	cases := []struct{ action, in, secret, want string }{
		// echo -n "hello" | shasum -a 256
		{"sha256", "hello", "", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		// echo -n "hello" | md5
		{"md5", "hello", "", "5d41402abc4b2a76b9719d911017c592"},
		// echo -n "hello" | shasum -a 1
		{"sha1", "hello", "", "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		// echo -n "hello" | openssl dgst -sha256 -hmac "key"
		{"hmac-sha256", "hello", "key", "9307b3b915efb5171ff14d8cb55fbcc798c6c0ef1456d66ded1a6aa723a58b7b"},
		{"base64", "hello", "", "aGVsbG8="},
		{"base64decode", "aGVsbG8=", "", "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			rc := engine.NewRunContext("r1", nil)
			rc.Set("n1", tc.in)
			node := models.WorkflowNode{
				ID: "c1", Type: models.NodeTypeTool, Template: "crypto",
				Config:  map[string]string{"cryptoAction": tc.action},
				Secrets: map[string]string{"cryptoSecret": tc.secret},
			}
			got, err := nodes.ExecuteTool(context.Background(), node, rc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%s: want %q, got %q", tc.action, tc.want, got)
			}
		})
	}
}

func TestCryptoRejectsUnknownAction(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "hello")
	node := models.WorkflowNode{
		ID: "c1", Type: models.NodeTypeTool, Template: "crypto",
		Config: map[string]string{"cryptoAction": "rot13"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error for an unsupported action, got nil")
	}
}

func TestCryptoHMACRequiresSecret(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	rc.Set("n1", "hello")
	node := models.WorkflowNode{
		ID: "c1", Type: models.NodeTypeTool, Template: "crypto",
		Config: map[string]string{"cryptoAction": "hmac-sha256"},
	}
	if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
		t.Error("want an error when hmac has no secret, got nil")
	}
}

func TestDateTimeDefaultIsUnchangedRFC3339(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{ID: "d1", Type: models.NodeTypeTool, Template: "datetime"}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("want a string, got %T", got)
	}
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Errorf("default output must stay RFC3339 (existing workflows depend on it): %q", s)
	}
	if !strings.HasSuffix(s, "Z") {
		t.Errorf("default output must stay UTC, got %q", s)
	}
}

func TestDateTimeAppliesOffsetZoneAndFormat(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "d1", Type: models.NodeTypeTool, Template: "datetime",
		Config: map[string]string{"dtFormat": "date", "dtZone": "Asia/Kolkata", "dtOffset": "-24h"},
	}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().Add(-24 * time.Hour).In(mustZone(t, "Asia/Kolkata")).Format("2006-01-02")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestDateTimeUnixFormat(t *testing.T) {
	rc := engine.NewRunContext("r1", nil)
	node := models.WorkflowNode{
		ID: "d1", Type: models.NodeTypeTool, Template: "datetime",
		Config: map[string]string{"dtFormat": "unix"},
	}
	got, err := nodes.ExecuteTool(context.Background(), node, rc)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.ParseInt(got.(string), 10, 64)
	if err != nil {
		t.Fatalf("want a unix timestamp, got %v", got)
	}
	if delta := time.Since(time.Unix(n, 0)); delta > time.Minute || delta < -time.Minute {
		t.Errorf("timestamp is not close to now: %v", delta)
	}
}

func TestDateTimeRejectsBadConfig(t *testing.T) {
	for _, cfg := range []map[string]string{
		{"dtZone": "Mars/Olympus"},
		{"dtOffset": "tomorrow"},
	} {
		rc := engine.NewRunContext("r1", nil)
		node := models.WorkflowNode{ID: "d1", Type: models.NodeTypeTool, Template: "datetime", Config: cfg}
		if _, err := nodes.ExecuteTool(context.Background(), node, rc); err == nil {
			t.Errorf("want an error for config %v, got nil", cfg)
		}
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tzdata unavailable for %s: %v", name, err)
	}
	return loc
}
