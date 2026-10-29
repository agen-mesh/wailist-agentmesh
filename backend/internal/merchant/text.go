package merchant

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type TextResult struct {
	SHA256     string `json:"sha256"`
	Bytes      int    `json:"bytes"`
	CodePoints int    `json:"codePoints"`
	Words      int    `json:"words"`
	Lines      int    `json:"lines"`
}

func TextDigest(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text, err := ParseRequest(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(TextResult{
		SHA256: Digest([]byte(text)), Bytes: len(text), CodePoints: utf8.RuneCountInString(text),
		Words: len(strings.Fields(text)), Lines: strings.Count(text, "\n") + 1,
	})
}
