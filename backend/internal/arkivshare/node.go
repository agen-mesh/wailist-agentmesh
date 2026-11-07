package arkivshare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type nodeWriter struct {
	scriptPath string
}

func NewNodeWriter(scriptPath string) Writer {
	return nodeWriter{scriptPath: scriptPath}
}

var nodeEntityKey = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// childEnv is the only environment the writer process receives. The script
// loads the Arkiv SDK, so it must not inherit the database URL, JWT secret,
// or any other server credential. PATH is required for the script's
// "#!/usr/bin/env node" shebang.
func childEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"ARKIV_SHARE_PRIVATE_KEY=" + os.Getenv("ARKIV_SHARE_PRIVATE_KEY"),
		"ARKIV_ACCESS_KEY=" + os.Getenv("ARKIV_ACCESS_KEY"),
	}
}

func (w nodeWriter) CreateShare(ctx context.Context, payload []byte) (Receipt, error) {
	cmd := exec.CommandContext(ctx, w.scriptPath)
	cmd.Env = childEnv()
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if strings.TrimSpace(stderr.String()) == "share_disabled" {
		return Receipt{}, ErrDisabled
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("%w", ErrUnconfirmed)
	}
	var out struct {
		EntityKey string `json:"entityKey"`
		TxHash    string `json:"txHash"`
	}
	if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out) != nil ||
		!nodeEntityKey.MatchString(out.EntityKey) ||
		!nodeEntityKey.MatchString(out.TxHash) {
		return Receipt{}, fmt.Errorf("%w", ErrUnconfirmed)
	}
	return Receipt{EntityKey: out.EntityKey, TxHash: out.TxHash}, nil
}
