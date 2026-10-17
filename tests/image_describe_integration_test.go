package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/cli"
	"github.com/BlueSkyXN/XRAG/internal/store"
	"github.com/BlueSkyXN/XRAG/protocols/mcp"
)

func imageRequest(kb string) map[string]any {
	return map[string]any{"kb": kb, "prompt": "描述图片", "images": []any{map[string]any{"mime": "image/png", "data_base64": "AQIDBA=="}}}
}

func imageCLI(t *testing.T, h productTest) []string {
	t.Helper()
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	must(t, os.WriteFile(key, []byte(h.key), 0600))
	return []string{"--config", filepath.Join(dir, "client.json"), "--server", h.url, "--key-file", key}
}

func TestImageDescribeCLIExplicitKBAndJSON(t *testing.T) {
	h, calls := describeSetup(t)
	must(t, h.s.CreateKB(ctx, "target", "Target"))
	file := filepath.Join(t.TempDir(), "panel.png")
	must(t, os.WriteFile(file, []byte{1, 2, 3, 4}, 0600))
	for _, args := range [][]string{
		{"image", "describe", "--kb", "target", "--images", file, "描述图片"},
		{"image", "describe", "--data", core.JSON(imageRequest("target"))},
	} {
		var out, errout bytes.Buffer
		must(t, cli.Main(append(imageCLI(t, h), args...), &out, &errout))
		if object(t, out.Bytes())["kb"] != "target" {
			t.Fatal(out.String())
		}
	}
	if len(*calls) != 2 {
		t.Fatal("expected two model calls", len(*calls))
	}
}

func TestImageDescribeCLIDryRunRejectsOversizedImage(t *testing.T) {
	h, calls := describeSetup(t)
	file := filepath.Join(t.TempDir(), "large.png")
	must(t, os.WriteFile(file, make([]byte, 525001), 0600))
	var out, errout bytes.Buffer
	err := cli.Main(append(imageCLI(t, h), "image", "describe", "--images", file, "--dry-run", "describe"), &out, &errout)
	if err == nil || len(*calls) != 0 {
		t.Fatal("oversized image must fail before a model call", err, len(*calls))
	}
}

func TestImageDescribeMCPHTTPResultMatchesAdvertisedSchema(t *testing.T) {
	h, calls := describeSetup(t)
	n, _, body := postMCP9(t, h.url+"/mcp", h.key, rpc9("tools/list", map[string]any{}), "application/json")
	if n != 200 {
		t.Fatal(n, string(body))
	}
	var schema map[string]any
	for _, item := range object(t, body)["result"].(map[string]any)["tools"].([]any) {
		tool := item.(map[string]any)
		if tool["name"] == "image_describe" {
			schema = tool["outputSchema"].(map[string]any)
		}
	}
	if schema == nil {
		t.Fatal("image_describe not advertised")
	}
	n, _, body = postMCP9(t, h.url+"/mcp", h.key, rpc9("tools/call", map[string]any{"name": "image_describe", "arguments": imageRequest("demo")}), "application/json")
	result := object(t, body)["result"].(map[string]any)
	if n != 200 || result["isError"] != false || len(*calls) != 1 {
		t.Fatal(n, string(body), len(*calls))
	}
	must(t, contracts.Validate(schema, result["structuredContent"]))
}

func TestImageDescribeMCPStdioUsesSingleKBPayload(t *testing.T) {
	h, calls := describeSetup(t)
	input := filepath.Join(t.TempDir(), "input.jsonl")
	args := imageRequest("demo")
	delete(args, "kb")
	must(t, os.WriteFile(input, append(rpc9("tools/call", map[string]any{"name": "image_describe", "arguments": args}), '\n'), 0600))
	in, err := os.Open(input)
	must(t, err)
	defer in.Close()
	previous := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = previous }()
	var out, errout bytes.Buffer
	must(t, cli.Main(append(imageCLI(t, h), "mcp"), &out, &errout))
	result := object(t, out.Bytes())["result"].(map[string]any)
	if result["isError"] != false || len(*calls) != 1 {
		t.Fatal(out.String(), len(*calls))
	}
}

func TestImageDescribeBudgetBlocksBeforeSend(t *testing.T) {
	for _, policy := range []store.BudgetPolicy{{MaxTokens: 1}, {DailyTokens: 1}, {MaxCost: 1}} {
		t.Run(core.JSON(policy), func(t *testing.T) {
			h, calls := describeSetup(t)
			_, err := h.s.SetKBSettings(ctx, "demo", store.KBSettings{Budget: policy}, 0)
			must(t, err)
			n, body := h.request(t, "POST", "/v1/describe", imageRequest("demo"), h.key)
			want := "BUDGET_EXCEEDED"
			if policy.MaxCost > 0 {
				want = "BUDGET_PRICE_MISSING"
			}
			if n < 400 || !strings.Contains(string(body), want) || len(*calls) != 0 {
				t.Fatal(n, string(body), len(*calls))
			}
		})
	}
}

func TestImageDescribeRecordsUsageWithoutImageData(t *testing.T) {
	h, _ := describeSetup(t)
	n, body := h.request(t, "POST", "/v1/describe", imageRequest("demo"), h.key)
	if n != 200 {
		t.Fatal(n, string(body))
	}
	n, body = h.request(t, "GET", "/v1/kbs/demo/usage", nil, h.key)
	if n != 200 {
		t.Fatal(n, string(body))
	}
	var report struct {
		Recent []struct {
			Operation string `json:"operation"`
			Tokens    int    `json:"charged_tokens"`
		} `json:"recent"`
	}
	must(t, json.Unmarshal(body, &report))
	if len(report.Recent) != 1 || report.Recent[0].Operation != "chat" || report.Recent[0].Tokens != 5 || strings.Contains(string(body), "AQIDBA==") {
		t.Fatal(string(body))
	}
}

func TestImageDescribeMCPRejectsUnauthorizedAndOverBudgetCalls(t *testing.T) {
	h, calls := describeSetup(t)
	must(t, h.s.CreateKB(ctx, "other", "Other"))
	reader, err := h.s.CreateKey(ctx, "image reader", "reader", []string{"demo"})
	must(t, err)
	check := func(args map[string]any, want string) {
		t.Helper()
		n, _, body := postMCP9(t, h.url+"/mcp", reader, rpc9("tools/call", map[string]any{"name": "image_describe", "arguments": args}), "application/json")
		result := object(t, body)["result"].(map[string]any)
		if n != 200 || result["isError"] != true || result["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != want || len(*calls) != 0 {
			t.Fatal(n, string(body), len(*calls))
		}
	}
	check(imageRequest("other"), "PERMISSION_DENIED")
	bad := imageRequest("demo")
	bad["images"] = []any{map[string]any{"mime": "image/png", "data_base64": "!!!"}}
	check(bad, "INVALID_ARGUMENT")
	_, err = h.s.SetKBSettings(ctx, "demo", store.KBSettings{Budget: store.BudgetPolicy{DailyTokens: 1}}, 0)
	must(t, err)
	check(imageRequest("demo"), "BUDGET_EXCEEDED")
}

func TestImageDescribeCLIRejectsAmbiguousKB(t *testing.T) {
	h, calls := describeSetup(t)
	var out, errout bytes.Buffer
	err := cli.Main(append(imageCLI(t, h), "image", "describe", "--kb", "demo,other", "--data", core.JSON(imageRequest("demo"))), &out, &errout)
	code(t, err, "INVALID_ARGUMENT")
	if len(*calls) != 0 {
		t.Fatal("ambiguous KB sent to model")
	}
}

func TestImageDescribeCLIRejectsNullData(t *testing.T) {
	h, calls := describeSetup(t)
	var out, errout bytes.Buffer
	err := cli.Main(append(imageCLI(t, h), "image", "describe", "--kb", "demo", "--data", "null"), &out, &errout)
	code(t, err, "INVALID_ARGUMENT")
	if len(*calls) != 0 {
		t.Fatal("invalid request sent to model")
	}
}

func TestImageDescribeMCPRequiresServerCapability(t *testing.T) {
	s := mcp.Server{Toolsets: "core"}
	for _, tool := range s.Tools() {
		if tool["name"] == "image_describe" {
			t.Fatal("image tool advertised without a Server executor")
		}
	}
	r := s.Handle(ctx, rpc9("tools/call", map[string]any{"name": "image_describe", "arguments": imageRequest("demo")})).(map[string]any)
	if r["error"] == nil {
		t.Fatal("unavailable image tool accepted", r)
	}
}
