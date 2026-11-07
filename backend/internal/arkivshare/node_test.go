package arkivshare_test

import (
	"context"
	"errors"
	"fmt"
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
	envdump := filepath.Join(dir, "env")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %q\ncat > %q\nenv > %q\nprintf '%%s\\n' '{\"entityKey\":\"0x%s\",\"txHash\":\"0x%s\"}'\n",
		args, capture, envdump, strings.Repeat("ab", 32), strings.Repeat("cd", 32))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARKIV_SHARE_PRIVATE_KEY", "0xabc")
	t.Setenv("ARKIV_ACCESS_KEY", "access-key")
	t.Setenv("DATABASE_URL", "postgres://leak")
	t.Setenv("JWT_SECRET", "jwt-leak")
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
	dumped, err := os.ReadFile(envdump)
	if err != nil {
		t.Fatal(err)
	}
	text := string(dumped)
	if !strings.Contains(text, "PATH=") || !strings.Contains(text, "ARKIV_SHARE_PRIVATE_KEY=0xabc") || !strings.Contains(text, "ARKIV_ACCESS_KEY=access-key") {
		t.Fatalf("writer env missing an allowlisted variable:\n%s", text)
	}
	if strings.Contains(text, "DATABASE_URL") || strings.Contains(text, "JWT_SECRET") || strings.Contains(text, "postgres://leak") {
		t.Fatalf("writer env inherited a server secret:\n%s", text)
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
