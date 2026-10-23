package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

type request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads one JSON-RPC message per line and writes one JSON line per response.
// Notifications, including notifications/initialized, produce no response.
func Serve(r io.Reader, w io.Writer, exec Executor) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := handleLine(w, exec, line); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handleLine(w io.Writer, exec Executor, line []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return writeMsg(w, envelope{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
	}
	id, hasID := raw["id"]
	if !hasID {
		return nil
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return writeMsg(w, envelope{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32700, Message: "parse error"}})
	}
	switch req.Method {
	case "initialize":
		return writeMsg(w, envelope{JSONRPC: "2.0", ID: id, Result: initializeResult(req.Params, exec)})
	case "tools/list":
		return writeMsg(w, envelope{JSONRPC: "2.0", ID: id, Result: map[string]any{"tools": exec.Definitions()}})
	case "tools/call":
		result := callTool(exec, req.Params)
		return writeMsg(w, envelope{JSONRPC: "2.0", ID: id, Result: result})
	default:
		return writeMsg(w, envelope{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32601, Message: "method not found"}})
	}
}

func initializeResult(params json.RawMessage, exec Executor) map[string]any {
	var body struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &body)
	name := ServerName
	if namer, ok := exec.(interface{ ServerName() string }); ok {
		if n := strings.TrimSpace(namer.ServerName()); n != "" {
			name = n
		}
	}
	return map[string]any{
		"protocolVersion": Negotiate(body.ProtocolVersion),
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": name, "version": ServerVersion},
	}
}

func callTool(exec Executor, params json.RawMessage) map[string]any {
	var body struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return toolError("ERR_INVALID_PATH: arguments")
	}
	dto, err := exec.Invoke(body.Name, body.Arguments)
	if err != nil {
		return toolError(err.Error())
	}
	text, err := json.Marshal(dto)
	if err != nil {
		return toolError(err.Error())
	}
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(text)}},
		"isError": false,
	}
}

func toolError(text string) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": true,
	}
}

func writeMsg(w io.Writer, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	// Encoder.Encode appends one newline. Refuse a second line.
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if bytes.Contains(out, []byte("\n")) {
		return io.ErrUnexpectedEOF
	}
	_, err := w.Write(append(out, '\n'))
	return err
}
