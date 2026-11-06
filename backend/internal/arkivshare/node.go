package arkivshare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func (w nodeWriter) CreateShare(ctx context.Context, payload []byte) (string, error) {
	cmd := exec.CommandContext(ctx, w.scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if strings.TrimSpace(stderr.String()) == "share_disabled" {
		return "", ErrDisabled
	}
	if err != nil {
		return "", fmt.Errorf("%w", ErrUnconfirmed)
	}
	var out struct {
		EntityKey string `json:"entityKey"`
		TxHash    string `json:"txHash"`
	}
	if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out) != nil || !nodeEntityKey.MatchString(out.EntityKey) {
		return "", fmt.Errorf("%w", ErrUnconfirmed)
	}
	return out.EntityKey, nil
}
