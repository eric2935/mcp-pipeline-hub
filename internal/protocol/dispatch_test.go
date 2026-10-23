package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type fakeExec struct{}

func (fakeExec) Definitions() []ToolDef {
	return []ToolDef{{Name: "iems_inspect_pipeline", Description: "inspect", InputSchema: map[string]any{"type": "object"}}}
}

func (fakeExec) Invoke(name string, args json.RawMessage) (any, error) {
	if name != "iems_inspect_pipeline" {
		return nil, errString("unknown")
	}
	return map[string]any{"current_branch": "feature/IEMS-451"}, nil
}

type errString string

func (e errString) Error() string { return string(e) }

func TestServeHandshakeIsSingleLine(t *testing.T) {
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"iems_inspect_pipeline","arguments":{}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in), &out, fakeExec{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses %d, body %s", len(lines), out.String())
	}
	var init map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	result := init["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("version %#v", result["protocolVersion"])
	}
	var list map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	tools := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools %#v", tools)
	}
	var call map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &call); err != nil {
		t.Fatal(err)
	}
	callResult := call["result"].(map[string]any)
	if callResult["isError"] != false {
		t.Fatalf("call %#v", callResult)
	}
	content := callResult["content"].([]any)[0].(map[string]any)
	if !strings.Contains(content["text"].(string), "feature/IEMS-451") {
		t.Fatalf("text %#v", content["text"])
	}
}

func TestNegotiateUnknownVersion(t *testing.T) {
	if Negotiate("1999-01-01") != ProtocolDefault {
		t.Fatal(Negotiate("1999-01-01"))
	}
}
