package handlers

import "testing"

// TestIsSystemConsoleWorkflowName pins the filter ListWorkflows applies so a
// partner console's hidden row (Tendril, Prism) never shows up next to real
// workflows in the list -- it is an endpoint you test in a sandbox, not
// something the user authored or can open on a canvas.
func TestIsSystemConsoleWorkflowName(t *testing.T) {
	for _, name := range []string{tendrilConsoleWorkflowName, prismConsoleWorkflowName} {
		if !isSystemConsoleWorkflowName(name) {
			t.Errorf("%q should be recognised as a system console workflow", name)
		}
	}

	for _, name := range []string{
		"",
		"My workflow",
		"Prism Console",                          // missing the "(managed, do not edit)" suffix
		"Tendril Console (managed, do not edit)", // Prism's punctuation on Tendril's name
		"prism console (managed, do not edit)",   // wrong case
	} {
		if isSystemConsoleWorkflowName(name) {
			t.Errorf("%q was treated as a system console workflow; only an exact match should be", name)
		}
	}
}
