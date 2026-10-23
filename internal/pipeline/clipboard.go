package pipeline

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const clipboardTimeout = 5 * time.Second

func clipboardArgsEmpty(in ingestIn) bool {
	return strings.TrimSpace(in.TicketID) == "" &&
		strings.TrimSpace(in.Scope) == "" &&
		strings.TrimSpace(in.Title) == "" &&
		strings.TrimSpace(in.SpecMarkdown) == ""
}

// IngestClipboard reads a Spec from ReadClipboard and writes it.
// A malformed spec does not create a file or edit TODO.md.
func (h *Hub) IngestClipboard(ctx context.Context) (ingestOut, error) {
	if h.ReadClipboard == nil {
		return ingestOut{}, &ToolError{Code: "ERR_CLIPBOARD"}
	}
	text, err := h.ReadClipboard(ctx)
	if err != nil {
		return ingestOut{}, clipboardFailure(err)
	}
	if strings.TrimSpace(text) == "" {
		return ingestOut{}, &ToolError{Code: "ERR_CLIPBOARD"}
	}
	in, err := h.parseClipboardSpec(text)
	if err != nil {
		return ingestOut{}, err
	}
	return h.IngestSpec(in)
}

func (h *Hub) parseClipboardSpec(raw string) (ingestIn, error) {
	body, err := stripToFrontmatter(raw)
	if err != nil {
		return ingestIn{}, err
	}
	header := firstSpecHeader(body)
	match := h.cfg().SpecHeaderPattern().FindStringSubmatch(header)
	if match == nil {
		return ingestIn{}, &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	ticket := match[1]
	scope := match[2]
	title := strings.TrimSpace(match[3])
	commitType := frontmatterScalar(body, "commit_type")
	if commitType == "" {
		commitType = "Feat"
	}
	todo, err := h.todoLineForSpec(body, ticket, scope, title, commitType)
	if err != nil {
		return ingestIn{}, err
	}
	return ingestIn{
		TicketID:     ticket,
		Scope:        scope,
		Title:        title,
		SpecMarkdown: body,
		TodoLine:     todo,
	}, nil
}

func firstSpecHeader(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "# [Spec] ") {
			return trim
		}
	}
	return ""
}

func frontmatterScalar(content, key string) string {
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	prefix := key + ":"
	for _, line := range strings.Split(rest[:end], "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return strings.Trim(value, `"'`)
	}
	return ""
}

func (h *Hub) todoLineForSpec(body, ticket, scope, title, commitType string) (string, error) {
	if line, ok := targetTodoLine(body); ok {
		return h.validateTodoLine(line, ticket)
	}
	return h.validateTodoLine("- [ ] "+ticket+": ["+commitType+"]("+scope+"): "+title, ticket)
}

func targetTodoLine(body string) (string, bool) {
	const prefix = "> Target Todo:"
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trim, prefix)), true
		}
	}
	return "", false
}

func todoLineForbidden(line string) bool {
	return strings.Contains(line, "(Spec:") || strings.Contains(line, "SPEC - [")
}

func (h *Hub) validateTodoLine(line, ticket string) (string, error) {
	match := h.cfg().TodoTicketPattern().FindStringSubmatch(line)
	if match == nil || match[1] != ticket || todoLineForbidden(line) {
		return "", &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	return line, nil
}

func clipboardFailure(err error) error {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}
	return &ToolError{Code: "ERR_CLIPBOARD"}
}

func readOSClipboard(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, clipboardTimeout)
	defer cancel()
	name, args, err := clipboardCommand(runtime.GOOS, exec.LookPath)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", clipboardFailure(err)
	}
	return string(out), nil
}

func clipboardCommand(goos string, lookPath func(string) (string, error)) (string, []string, error) {
	switch goos {
	case "darwin":
		return "pbpaste", nil, nil
	case "windows":
		return "powershell", []string{"-NoProfile", "-Command", "Get-Clipboard"}, nil
	case "linux":
		if _, err := lookPath("wl-paste"); err == nil {
			return "wl-paste", nil, nil
		}
		if _, err := lookPath("xclip"); err == nil {
			return "xclip", []string{"-selection", "clipboard", "-o"}, nil
		}
		return "", nil, &ToolError{Code: "ERR_CLIPBOARD"}
	default:
		return "", nil, &ToolError{Code: "ERR_CLIPBOARD"}
	}
}
