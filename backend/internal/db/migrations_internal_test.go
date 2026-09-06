package db

import (
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// A duplicate migration version is a boot failure, not a test failure: iofs
// rejects the whole source, so New() returns before a single migration runs and
// the server never starts. Nothing catches that today until someone runs the
// binary.
//
// It is an easy mistake to make, and it has already happened twice on one
// branch: a version that was free when the branch was cut gets taken by
// whatever merges to master first, and the branch only finds out at startup.
// This turns that into a failing test.
//
// No database required -- it reads the embedded FS and nothing else.
func TestMigrationVersionsAreUnique(t *testing.T) {
	if _, err := iofs.New(migrationsFS, "migrations"); err != nil {
		t.Fatalf("migration source rejected: %v\n\n"+
			"A duplicate version means the backend cannot start at all. If this "+
			"appeared after merging master, renumber this branch's migrations "+
			"above every version master now uses.", err)
	}
}
