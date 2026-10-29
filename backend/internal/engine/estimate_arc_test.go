package engine

import (
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/models"
)

func TestArcEstimateDoesNotDebitCredits(t *testing.T) {
	est := EstimateRunCost(models.Workflow{Nodes: []models.WorkflowNode{{
		ID: "arc1", Type: models.NodeTypeArc, Template: "nanopay", Name: "Arc Nanopay", Price: "0.01",
	}}})
	if est.LowUSDMicros != 0 || est.HighUSDMicros != 0 {
		t.Fatalf("credits = %d/%d", est.LowUSDMicros, est.HighUSDMicros)
	}
	if len(est.Lines) != 1 || !strings.Contains(est.Lines[0].Note, "USDC") {
		t.Fatalf("lines = %+v", est.Lines)
	}
}
