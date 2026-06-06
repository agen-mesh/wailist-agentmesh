package engine

import (
	"errors"

	"github.com/agentmesh/backend/internal/models"
)

// TopologicalSort returns nodes grouped into parallel execution levels using Kahn's algorithm.
// Only flow edges determine order; attach edges are ignored.
func TopologicalSort(nodes []models.WorkflowNode, edges []models.WorkflowEdge) ([][]models.WorkflowNode, error) {
	nodeMap := make(map[string]models.WorkflowNode, len(nodes))
	inDegree := make(map[string]int, len(nodes))
	successors := make(map[string][]string)

	for _, n := range nodes {
		nodeMap[n.ID] = n
		inDegree[n.ID] = 0
	}

	for _, e := range edges {
		if e.Kind != models.EdgeKindFlow {
			continue
		}
		if _, ok := nodeMap[e.From]; !ok {
			continue
		}
		if _, ok := nodeMap[e.To]; !ok {
			continue
		}
		successors[e.From] = append(successors[e.From], e.To)
		inDegree[e.To]++
	}

	queue := make([]string, 0)
	for _, n := range nodes {
		if inDegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}

	var levels [][]models.WorkflowNode
	visited := 0

	for len(queue) > 0 {
		level := make([]models.WorkflowNode, 0, len(queue))
		next := make([]string, 0)
		for _, id := range queue {
			level = append(level, nodeMap[id])
			visited++
			for _, succ := range successors[id] {
				inDegree[succ]--
				if inDegree[succ] == 0 {
					next = append(next, succ)
				}
			}
		}
		levels = append(levels, level)
		queue = next
	}

	if visited != len(nodes) {
		return nil, errors.New("cycle detected in workflow graph")
	}
	return levels, nil
}

// BuildAttachMap maps each agent node ID to its attached provider and tools.
func BuildAttachMap(nodes []models.WorkflowNode, edges []models.WorkflowEdge) map[string]models.AttachConfig {
	nodeMap := make(map[string]models.WorkflowNode, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	result := make(map[string]models.AttachConfig)
	for _, e := range edges {
		if e.Kind != models.EdgeKindAttach {
			continue
		}
		cfg := result[e.To]
		src, ok := nodeMap[e.From]
		if !ok {
			continue
		}
		switch e.ToPort {
		case "model":
			s := src
			cfg.Provider = &s
		case "tools":
			cfg.Tools = append(cfg.Tools, src)
		}
		result[e.To] = cfg
	}
	return result
}
