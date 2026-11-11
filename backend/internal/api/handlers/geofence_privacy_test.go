package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

const (
	privacyZoneLat = 51.5007
	privacyZoneLng = -0.1246
)

func geofencePrivacyWorkflow(t *testing.T) (*handlers.Deps, models.Workflow) {
	t.Helper()
	t.Setenv("DISCORD_WEBHOOK_WORKFLOWS_URL", "")
	t.Setenv("FCM_SERVICE_ACCOUNT_JSON", "")
	d := testDeps(t)
	d.Engine = engine.NewRunner(d.Store, sse.NewBroker(), nil, "", "", testEncryptionKey, engine.X402Config{})
	wf, err := d.Store.CreateWorkflow(t.Context(), "Geofence privacy", "dev")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Engine.Stop(wf.ID)
		waitForGeofenceRun(t, d, wf.ID)
		if err := d.Store.DeleteWorkflow(context.Background(), wf.ID); err != nil {
			t.Error(err)
		}
	})
	_, err = d.Store.UpdateWorkflow(t.Context(), wf.ID, wf.Name, models.WorkflowGraph{
		Nodes: []models.WorkflowNode{{ID: "crossing", Type: models.NodeTypeTrigger}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Store.SetWorkflowDeployed(t.Context(), wf.ID, "https://example.invalid/run", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.SetWorkflowGeofence(t.Context(), wf.ID, privacyZoneLat, privacyZoneLng, 100); err != nil {
		t.Fatal(err)
	}
	return d, wf
}

func waitForGeofenceRun(t *testing.T, d *handlers.Deps, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for d.Engine.IsRunning(id) {
		if time.Now().After(deadline) {
			t.Fatal("local crossing run did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func sendPrivacyPing(t *testing.T, d *handlers.Deps, workflowID, userID string, lat, lng float64, at time.Time) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"lat": lat, "lng": lng, "accuracyM": 7.654321, "recordedAt": at,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/workflows/"+workflowID+"/location", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), handlers.CtxUserID, userID))
	req = withURLParam(req, "id", workflowID)
	w := httptest.NewRecorder()
	d.LocationPing(w, req)
	return w
}

func assertPrivacyZone(t *testing.T, wf models.Workflow) {
	t.Helper()
	if wf.GeofenceLat == nil || *wf.GeofenceLat != privacyZoneLat ||
		wf.GeofenceLng == nil || *wf.GeofenceLng != privacyZoneLng ||
		wf.GeofenceRadiusM == nil || *wf.GeofenceRadiusM != 100 {
		t.Error("crossing ingest changed the configured zone")
	}
}

func TestGeofencePrivacyCrossings(t *testing.T) {
	for _, tc := range []struct {
		direction string
		lat       float64
		inside    bool
	}{
		{"enter", 51.500711122, true},
		{"leave", 51.502711122, false},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			d, wf := geofencePrivacyWorkflow(t)
			at := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			if _, err := d.Store.RecordGeofenceFix(t.Context(), wf.ID, !tc.inside, at.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			w := sendPrivacyPing(t, d, wf.ID, "dev", tc.lat, -0.124622233, at)
			if w.Code != http.StatusAccepted {
				t.Fatalf("crossing: status %d, %s", w.Code, w.Body.String())
			}
			var result struct {
				RunID     string `json:"runId"`
				Direction string `json:"direction"`
				Inside    bool   `json:"inside"`
				Triggered bool   `json:"triggered"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.RunID == "" || result.Direction != tc.direction || result.Inside != tc.inside || !result.Triggered {
				t.Fatalf("wrong crossing response: %+v", result)
			}
			waitForGeofenceRun(t, d, wf.ID)
			run, err := d.Store.GetRun(t.Context(), result.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != models.RunStatusSuccess {
				t.Fatalf("local trigger run did not succeed: %s", run.Status)
			}
			want := map[string]any{"trigger": "geofence", "direction": tc.direction, "recordedAt": at.Format(time.RFC3339Nano)}
			if !reflect.DeepEqual(run.InputContext, want) {
				t.Errorf("persisted run input must contain only event metadata: got %#v", run.InputContext)
			}
			logs, err := d.Store.GetRunLogs(t.Context(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(logs) != 1 || logs[0].NodeID != "crossing" || logs[0].Status != models.LogStatusSuccess {
				t.Fatalf("expected a completed trigger log, got %+v", logs)
			}
			if !reflect.DeepEqual(logs[0].Output, want) {
				t.Errorf("persisted trigger output must contain only event metadata: got %#v", logs[0].Output)
			}
			stored, err := d.Store.GetWorkflow(t.Context(), wf.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertPrivacyZone(t, stored)
			if stored.GeofenceInside == nil || *stored.GeofenceInside != tc.inside || stored.GeofenceLastFixAt == nil || !stored.GeofenceLastFixAt.Equal(at) {
				t.Error("derived crossing state and time were not retained")
			}
		})
	}
}

func TestGeofencePrivacyNonCrossings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		user     string
		baseline bool
		stale    bool
		lat      float64
		status   int
	}{
		{"baseline", "dev", false, false, 51.500711122, http.StatusOK},
		{"same-side", "dev", true, false, 51.500711122, http.StatusOK},
		{"stale-crossing", "dev", true, true, 51.502711122, http.StatusOK},
		{"invalid-position", "dev", true, false, 91, http.StatusBadRequest},
		{"other-owner", "owner", true, false, 51.502711122, http.StatusNotFound},
		{"missing-owner", "", true, false, 51.502711122, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, wf := geofencePrivacyWorkflow(t)
			baselineAt := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
			if tc.baseline {
				if _, err := d.Store.RecordGeofenceFix(t.Context(), wf.ID, true, baselineAt); err != nil {
					t.Fatal(err)
				}
			}
			at := baselineAt.Add(time.Minute)
			if tc.stale {
				at = baselineAt.Add(-time.Minute)
			}
			w := sendPrivacyPing(t, d, wf.ID, tc.user, tc.lat, -0.124622233, at)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == http.StatusOK {
				var result map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result["triggered"] != false || result["stale"] != tc.stale {
					t.Errorf("unexpected no-run result: %#v", result)
				}
			}
			runs, err := d.Store.ListWorkflowRuns(t.Context(), "dev", wf.ID, nil, 10)
			if err != nil || len(runs) != 0 {
				t.Fatalf("non-crossing created run data: runs=%v err=%v", runs, err)
			}
			stored, err := d.Store.GetWorkflow(t.Context(), wf.ID)
			if err != nil {
				t.Fatal(err)
			}
			assertPrivacyZone(t, stored)
			wantAt := at
			if tc.stale || tc.status != http.StatusOK {
				wantAt = baselineAt
			}
			if stored.GeofenceInside == nil || !*stored.GeofenceInside || stored.GeofenceLastFixAt == nil || !stored.GeofenceLastFixAt.Equal(wantAt) {
				t.Error("accepted state was lost or a rejected fix changed it")
			}
		})
	}
}
