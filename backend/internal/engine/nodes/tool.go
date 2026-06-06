package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

func ExecuteTool(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	switch node.Template {
	case "calc":
		return evalMath(node.URL)
	case "datetime":
		return time.Now().UTC().Format(time.RFC3339), nil
	case "http":
		return callHTTP(ctx, node, rc)
	default:
		return rc.Message(), nil
	}
}

func callHTTP(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	method := node.Method
	if method == "" {
		method = http.MethodGet
	}
	var bodyReader io.Reader
	if method == http.MethodPost {
		bodyReader = bytes.NewReader([]byte(rc.Message()))
	}
	req, err := http.NewRequestWithContext(ctx, method, node.URL, bodyReader)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result any
	if json.Unmarshal(b, &result) == nil {
		return result, nil
	}
	return string(b), nil
}

func evalMath(expr string) (string, error) {
	fset := token.NewFileSet()
	tv, err := types.Eval(fset, nil, token.NoPos, expr)
	if err != nil {
		return "", fmt.Errorf("calc: %w", err)
	}
	if tv.Value == nil {
		return "", fmt.Errorf("calc: nil result")
	}
	if tv.Value.Kind() == constant.Int {
		return tv.Value.String(), nil
	}
	f, _ := strconv.ParseFloat(tv.Value.String(), 64)
	return strconv.FormatFloat(f, 'f', -1, 64), nil
}
