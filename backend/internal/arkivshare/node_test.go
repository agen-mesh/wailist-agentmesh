package arkivshare_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/arkivshare"
)

func TestNodeWriterPassesPayloadOnStdinOnly(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "writer.sh")
	capture := filepath.Join(dir, "in")
	args := filepath.Join(dir, "args")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$ARKIV_TEST_ARGS\"\ncat > \"$ARKIV_TEST_CAPTURE\"\nprintf '%s\\n' '{\"entityKey\":\"0x"+strings.Repeat("ab", 32)+"\",\"txHash\":\"0x"+strings.Repeat("cd", 32)+"\"}'\n"), 0o755)
	t.Setenv("ARKIV_TEST_CAPTURE", capture)
	t.Setenv("ARKIV_TEST_ARGS", args)
	t.Setenv("ARKIV_SHARE_PRIVATE_KEY", "0xabc")
	w := arkivshare.NewNodeWriter(script)
	key, err := w.CreateShare(context.Background(), []byte("graph"))
	if err != nil || key != "0x"+strings.Repeat("ab", 32) {
		t.Fatalf("key %s err %v", key, err)
	}
	got, _ := os.ReadFile(capture)
	if string(got) != "graph" {
		t.Fatalf("stdin %q", got)
	}
	argv, _ := os.ReadFile(args)
	if len(argv) != 0 {
		t.Fatalf("argv %q", argv)
	}
}

func TestNodeWriterMapsUnconfirmedWithoutTheKey(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "writer.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' share_unconfirmed >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARKIV_SHARE_PRIVATE_KEY", "0xabc")
	w := arkivshare.NewNodeWriter(script)
	_, err := w.CreateShare(context.Background(), []byte("graph"))
	if !errors.Is(err, arkivshare.ErrUnconfirmed) {
		t.Fatalf("err = %v, want ErrUnconfirmed", err)
	}
	if strings.Contains(err.Error(), "0xabc") {
		t.Fatal(err)
	}
}
