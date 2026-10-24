package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mcp-pipeline-hub/internal/config"
	"mcp-pipeline-hub/internal/guard"
	"mcp-pipeline-hub/internal/protocol"
)

const (
	localGitTimeout = 15 * time.Second
	syncTimeout     = 30 * time.Second
	repomixTimeout  = 30 * time.Second

	repomixOutput = "repomix-output.xml"
)

// Runner executes a child process with stdout and stderr captured.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (stdout, stderr string, err error)
}

// ExecRunner uses os/exec. Child stdout is never attached to the server stdout.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// ToolError is a domain failure returned as an MCP isError text.
type ToolError struct {
	Code   string
	Detail string
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

// Hub is the five-tool executor rooted at a workspace.
type Hub struct {
	Root          string
	Run           Runner
	Now           func() time.Time
	ReadClipboard func(ctx context.Context) (string, error)
	Config        *config.PipelineConfig
}

// Open loads pipeline config and fails when the file is invalid or an explicit path is missing.
func Open(root, configPath string) (*Hub, error) {
	cfg, err := config.LoadConfig(root, configPath)
	if err != nil {
		return nil, err
	}
	return newHub(root, cfg), nil
}

// New loads config from root and falls back to the neutral defaults when the file is unusable.
func New(root string) *Hub {
	cfg, err := config.LoadConfig(root, "")
	if err != nil {
		cfg = config.DefaultConfig()
	}
	return newHub(root, cfg)
}

func newHub(root string, cfg *config.PipelineConfig) *Hub {
	return &Hub{Root: root, Run: ExecRunner{}, Now: time.Now, ReadClipboard: readOSClipboard, Config: cfg}
}

func (h *Hub) cfg() *config.PipelineConfig {
	if h == nil || h.Config == nil {
		return config.DefaultConfig()
	}
	return h.Config
}

func (h *Hub) specDirRel() string {
	return h.cfg().GetSpecActiveDir()
}

func (h *Hub) todoRel() string {
	return h.cfg().TodoFile
}

// ServerName is the MCP serverInfo name from tool_name.
func (h *Hub) ServerName() string {
	return h.cfg().ToolName
}

func defaultSpecRel() string {
	return config.DefaultConfig().GetSpecActiveDir()
}

func defaultPattern() *regexp.Regexp {
	return config.DefaultConfig().Pattern()
}

func (h *Hub) Definitions() []protocol.ToolDef {
	return []protocol.ToolDef{
		{
			Name:        "hub_pack_context",
			Description: "Pack the Code M context with npx repomix --no-gitignore and verify the SSOT manifests.",
			InputSchema: objectSchema(nil, nil),
		},
		{
			Name:        "hub_ingest_spec",
			Description: "Write a Spec under 03_Spec and optionally insert one idempotent TODO.md line. If ticket_id, scope, title, and spec_markdown are all empty, read the OS clipboard.",
			InputSchema: objectSchema(nil, map[string]any{
				"ticket_id":     map[string]any{"type": "string"},
				"scope":         map[string]any{"type": "string"},
				"title":         map[string]any{"type": "string"},
				"spec_markdown": map[string]any{"type": "string"},
				"todo_line":     map[string]any{"type": "string"},
			}),
		},
		{
			Name:        "hub_prepare_branch",
			Description: "MUST ONLY be called when preparing Phase 2 branch isolation.",
			InputSchema: objectSchema([]string{"ticket_id"}, map[string]any{
				"ticket_id":   map[string]any{"type": "string"},
				"slug":        map[string]any{"type": "string"},
				"sync_main":   map[string]any{"type": "boolean"},
				"allow_dirty": map[string]any{"type": "boolean"},
			}),
		},
		{
			Name:        "hub_inspect_pipeline",
			Description: "Report the current branch, active specs, and stageable files.",
			InputSchema: objectSchema(nil, nil),
		},
		{
			Name:        "hub_finalize_spec",
			Description: "CRITICAL GATE: MUST NEVER be called during Phase 3 or Phase 4. ONLY call after the human user explicitly approves verification completion (마감 승인).",
			InputSchema: objectSchema([]string{"ticket_id", "commit_type", "commit_scope", "commit_summary"}, map[string]any{
				"ticket_id":      map[string]any{"type": "string"},
				"commit_type":    map[string]any{"type": "string"},
				"commit_scope":   map[string]any{"type": "string"},
				"commit_summary": map[string]any{"type": "string"},
			}),
		},
	}
}

func objectSchema(required []string, props map[string]any) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (h *Hub) Invoke(name string, args json.RawMessage) (any, error) {
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	ctx := context.Background()
	switch canonicalTool(name) {
	case "pack":
		return h.PackContext(ctx)
	case "ingest":
		var in ingestIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, &ToolError{Code: "ERR_INVALID_PATH", Detail: "arguments"}
		}
		if clipboardArgsEmpty(in) {
			return h.IngestClipboard(ctx)
		}
		return h.IngestSpec(in)
	case "prepare":
		var in prepareIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, &ToolError{Code: "ERR_INVALID_PATH", Detail: "arguments"}
		}
		return h.PrepareBranch(ctx, in)
	case "inspect":
		return h.Inspect(ctx)
	case "finalize":
		var in finalizeIn
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, &ToolError{Code: "ERR_INVALID_PATH", Detail: "arguments"}
		}
		return h.Finalize(ctx, in)
	default:
		return nil, &ToolError{Code: "ERR_INVALID_PATH", Detail: "unknown tool"}
	}
}

// canonicalTool maps the public hub_* name and the legacy iems_* alias to one route.
func canonicalTool(name string) string {
	switch name {
	case "hub_pack_context", "iems_pack_context":
		return "pack"
	case "hub_ingest_spec", "iems_ingest_spec":
		return "ingest"
	case "hub_prepare_branch", "iems_prepare_branch":
		return "prepare"
	case "hub_inspect_pipeline", "iems_inspect_pipeline":
		return "inspect"
	case "hub_finalize_spec", "iems_finalize_spec":
		return "finalize"
	default:
		return ""
	}
}

type packOut struct {
	OutputPath   string `json:"output_path"`
	SizeBytes    int64  `json:"size_bytes"`
	SSOTVerified bool   `json:"ssot_verified"`
}

func (h *Hub) PackContext(ctx context.Context) (packOut, error) {
	ctx, cancel := context.WithTimeout(ctx, repomixTimeout)
	defer cancel()
	_, stderr, err := h.Run.Run(ctx, h.Root, "npx", "repomix", "--no-gitignore")
	if err != nil {
		return packOut{}, execErr(err, stderr)
	}
	outPath := filepath.Join(h.Root, repomixOutput)
	body, err := os.ReadFile(outPath)
	if err != nil {
		return packOut{}, &ToolError{Code: "ERR_SSOT_MISSING", Detail: repomixOutput}
	}
	text := string(body)
	for _, manifest := range h.cfg().SSOTManifests {
		if strings.TrimSpace(manifest) == "" || strings.Contains(text, manifest) {
			continue
		}
		return packOut{}, &ToolError{Code: "ERR_SSOT_MISSING", Detail: manifest}
	}
	info, err := os.Stat(outPath)
	if err != nil {
		return packOut{}, &ToolError{Code: "ERR_SSOT_MISSING", Detail: err.Error()}
	}
	return packOut{OutputPath: relSlash(h.Root, outPath), SizeBytes: info.Size(), SSOTVerified: true}, nil
}

type ingestIn struct {
	TicketID     string `json:"ticket_id"`
	Scope        string `json:"scope"`
	Title        string `json:"title"`
	SpecMarkdown string `json:"spec_markdown"`
	TodoLine     string `json:"todo_line"`
}

type ingestOut struct {
	SpecFilePath string `json:"spec_file_path"`
	TodoLine     string `json:"todo_line"`
	TodoUpdated  bool   `json:"todo_updated"`
}

func (h *Hub) IngestSpec(in ingestIn) (ingestOut, error) {
	if err := guard.ValidateTicketID(in.TicketID, h.cfg().Pattern()); err != nil {
		return ingestOut{}, asTool(err)
	}
	if err := guard.ValidatePathToken("scope", in.Scope); err != nil {
		return ingestOut{}, asTool(err)
	}
	if err := guard.ValidatePathToken("title", in.Title); err != nil {
		return ingestOut{}, asTool(err)
	}
	body, err := stripToFrontmatter(in.SpecMarkdown)
	if err != nil {
		return ingestOut{}, err
	}
	name := fmt.Sprintf("SPEC - [%s] [%s] %s.md", in.TicketID, in.Scope, in.Title)
	rel := h.specDirRel() + "/" + name
	full := filepath.Join(h.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ingestOut{}, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return ingestOut{}, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	updated := false
	if strings.TrimSpace(in.TodoLine) != "" {
		var uerr error
		updated, uerr = insertTodo(filepath.Join(h.Root, h.todoRel()), in.TicketID, in.TodoLine)
		if uerr != nil {
			return ingestOut{}, uerr
		}
	}
	return ingestOut{
		SpecFilePath: rel,
		TodoLine:     strings.TrimRight(in.TodoLine, "\r\n"),
		TodoUpdated:  updated,
	}, nil
}

func stripToFrontmatter(raw string) (string, error) {
	text := stripFence(strings.TrimSpace(raw))
	idx := strings.Index(text, "---")
	if idx < 0 {
		return "", &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	return text[idx:], nil
}

func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	if n < 3 {
		return s
	}
	rest := s[n:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return s
	}
	rest = rest[nl+1:]
	fence := strings.Repeat("`", n)
	rest = strings.TrimRight(rest, " \t\r\n")
	if strings.HasSuffix(rest, fence) {
		rest = strings.TrimRight(rest[:len(rest)-len(fence)], " \t\r\n")
	}
	return rest
}

func insertTodo(path, ticketID, line string) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return false, &ToolError{Code: "ERR_TODO_SECTION", Detail: err.Error()}
	}
	text := string(body)
	if ticketMentioned(text, ticketID) {
		return false, nil
	}
	lines := strings.Split(text, "\n")
	at := -1
	for i, row := range lines {
		if strings.TrimSpace(row) == "## In Progress" {
			at = i
			break
		}
	}
	if at < 0 {
		return false, &ToolError{Code: "ERR_TODO_SECTION"}
	}
	row := strings.TrimRight(line, "\r\n")
	next := make([]string, 0, len(lines)+1)
	next = append(next, lines[:at+1]...)
	next = append(next, row)
	next = append(next, lines[at+1:]...)
	out := strings.Join(next, "\n")
	if strings.HasSuffix(text, "\n") && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		return false, &ToolError{Code: "ERR_TODO_SECTION", Detail: err.Error()}
	}
	return true, nil
}

func ticketMentioned(text, id string) bool {
	re := regexp.MustCompile(`(?:^|[^A-Za-z0-9-])` + regexp.QuoteMeta(id) + `(?:[^0-9-]|$)`)
	return re.MatchString(text)
}

type prepareIn struct {
	TicketID   string `json:"ticket_id"`
	Slug       string `json:"slug"`
	SyncMain   *bool  `json:"sync_main"`
	AllowDirty *bool  `json:"allow_dirty"`
}

type prepareOut struct {
	BranchName string `json:"branch_name"`
	BaseCommit string `json:"base_commit"`
}

func (h *Hub) PrepareBranch(ctx context.Context, in prepareIn) (prepareOut, error) {
	branch, err := guard.BranchName(in.TicketID, in.Slug, h.cfg().Pattern())
	if err != nil {
		return prepareOut{}, asTool(err)
	}
	syncMain := true
	if in.SyncMain != nil {
		syncMain = *in.SyncMain
	}
	allowDirty := false
	if in.AllowDirty != nil {
		allowDirty = *in.AllowDirty
	}
	dirty, err := h.trackedDirty(ctx)
	if err != nil {
		return prepareOut{}, err
	}
	if !allowDirty {
		for _, p := range dirty {
			if !guard.IsBlacklisted(p, h.cfg().DirtyBlacklist) {
				return prepareOut{}, &ToolError{Code: "ERR_DIRTY_WORKTREE", Detail: p}
			}
		}
	}
	exists, err := h.branchExists(ctx, branch)
	if err != nil {
		return prepareOut{}, err
	}
	var hold *todoHold
	if syncMain && containsPath(dirty, h.todoRel()) {
		got, err := h.holdTodo(ctx)
		if err != nil {
			return prepareOut{}, err
		}
		hold = got
	}
	restore := func() error {
		return h.releaseTodo(ctx, hold)
	}
	fail := func(err error) (prepareOut, error) {
		if rerr := restore(); rerr != nil {
			return prepareOut{}, rerr
		}
		return prepareOut{}, err
	}
	if syncMain {
		if err := h.gitSync(ctx, "fetch", "origin"); err != nil {
			return fail(err)
		}
		if !exists {
			if err := h.gitLocal(ctx, "checkout", "main"); err != nil {
				return fail(err)
			}
			if err := h.gitSync(ctx, "pull", "--ff-only", "origin", "main"); err != nil {
				return fail(err)
			}
		}
	}
	if exists {
		if err := h.gitLocal(ctx, "checkout", branch); err != nil {
			return fail(err)
		}
	} else if err := h.gitLocal(ctx, "checkout", "-b", branch); err != nil {
		return fail(err)
	}
	if err := restore(); err != nil {
		return prepareOut{}, err
	}
	stdout, err := h.gitLocalOut(ctx, "rev-parse", "HEAD")
	if err != nil {
		return prepareOut{}, err
	}
	return prepareOut{BranchName: branch, BaseCommit: strings.TrimSpace(stdout)}, nil
}

type inspectOut struct {
	CurrentBranch      string   `json:"current_branch"`
	ActiveSpecs        []string `json:"active_specs"`
	StageableFiles     []string `json:"stageable_files"`
	ExcludedDirtyFiles []string `json:"excluded_dirty_files"`
}

func (h *Hub) Inspect(ctx context.Context) (inspectOut, error) {
	branch, err := h.gitLocalOut(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return inspectOut{}, err
	}
	dirty, err := h.trackedDirty(ctx)
	if err != nil {
		return inspectOut{}, err
	}
	stageable, excluded := guard.SplitDirty(dirty, h.cfg().DirtyBlacklist)
	sort.Strings(stageable)
	sort.Strings(excluded)
	specs, err := h.activeSpecs("")
	if err != nil {
		return inspectOut{}, err
	}
	return inspectOut{
		CurrentBranch:      strings.TrimSpace(branch),
		ActiveSpecs:        specs,
		StageableFiles:     stageable,
		ExcludedDirtyFiles: excluded,
	}, nil
}

type finalizeIn struct {
	TicketID      string `json:"ticket_id"`
	CommitType    string `json:"commit_type"`
	CommitScope   string `json:"commit_scope"`
	CommitSummary string `json:"commit_summary"`
}

type finalizeOut struct {
	ArchivedSpecPath string   `json:"archived_spec_path"`
	Mode             string   `json:"mode"`
	StageableFiles   []string `json:"stageable_files"`
	TerminalCommands string   `json:"terminal_commands"`
}

func (h *Hub) Finalize(ctx context.Context, in finalizeIn) (finalizeOut, error) {
	if err := guard.ValidateTicketID(in.TicketID, h.cfg().Pattern()); err != nil {
		return finalizeOut{}, asTool(err)
	}
	for _, field := range []struct{ name, val string }{
		{"commit_type", in.CommitType},
		{"commit_scope", in.CommitScope},
		{"commit_summary", in.CommitSummary},
	} {
		if strings.TrimSpace(field.val) == "" || hasControl(field.val) {
			return finalizeOut{}, &ToolError{Code: "ERR_INVALID_PATH", Detail: field.name}
		}
	}
	branch, err := h.gitLocalOut(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return finalizeOut{}, err
	}
	branch = strings.TrimSpace(branch)
	ok, err := guard.FinalizeBranchOK(branch, in.TicketID, h.cfg().Pattern())
	if err != nil {
		return finalizeOut{}, asTool(err)
	}
	if !ok {
		return finalizeOut{}, &ToolError{Code: "ERR_INVALID_BRANCH", Detail: branch}
	}
	matches, err := h.activeSpecs(in.TicketID)
	if err != nil {
		return finalizeOut{}, err
	}
	if len(matches) == 0 {
		return finalizeOut{}, &ToolError{Code: "ERR_SPEC_NOT_ACTIVE"}
	}
	if len(matches) > 1 {
		return finalizeOut{}, &ToolError{Code: "ERR_SPEC_AMBIGUOUS"}
	}
	srcRel := matches[0]
	src := filepath.Join(h.Root, filepath.FromSlash(srcRel))
	body, err := os.ReadFile(src)
	if err != nil {
		return finalizeOut{}, &ToolError{Code: "ERR_SPEC_NOT_ACTIVE", Detail: err.Error()}
	}
	date := h.Now().Format("2006-01-02")
	updated, err := completeFrontmatter(string(body), date, in)
	if err != nil {
		return finalizeOut{}, err
	}
	archiveDir := filepath.Join(h.Root, filepath.FromSlash(h.cfg().GetSpecArchiveDir()))
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return finalizeOut{}, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	dst := filepath.Join(archiveDir, filepath.Base(src))
	if err := os.WriteFile(src, []byte(updated), 0o644); err != nil {
		return finalizeOut{}, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	if err := os.Rename(src, dst); err != nil {
		return finalizeOut{}, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	stageable, err := h.commitCandidates(ctx)
	if err != nil {
		return finalizeOut{}, err
	}
	mode := "FAST_PATH"
	if len(stageable) > 0 {
		mode = "SURGICAL_PR"
	}
	return finalizeOut{
		ArchivedSpecPath: relSlash(h.Root, dst),
		Mode:             mode,
		StageableFiles:   stageable,
		TerminalCommands: closeTicketCommand,
	}, nil
}

func completeFrontmatter(content, date string, in finalizeIn) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", &ToolError{Code: "ERR_SPEC_MALFORMED"}
	}
	fm := rest[:end]
	body := rest[end:]
	lines := strings.Split(fm, "\n")
	values := map[string]string{
		"status":         "completed",
		"commit_type":    yamlDoubleQuote(in.CommitType),
		"commit_scope":   yamlDoubleQuote(in.CommitScope),
		"commit_summary": yamlDoubleQuote(in.CommitSummary),
		"updated_at":     date,
	}
	order := []string{"status", "commit_type", "commit_scope", "commit_summary", "updated_at"}
	found := map[string]bool{}
	for i, line := range lines {
		key, _, ok := splitFrontmatterKey(line)
		if !ok {
			continue
		}
		if val, exists := values[key]; exists {
			lines[i] = key + ": " + val
			found[key] = true
		}
	}
	for _, key := range order {
		if found[key] {
			continue
		}
		insertAt := 0
		for _, prev := range order {
			if prev == key {
				break
			}
			for i, line := range lines {
				k, _, ok := splitFrontmatterKey(line)
				if ok && k == prev {
					insertAt = i + 1
				}
			}
		}
		next := make([]string, 0, len(lines)+1)
		next = append(next, lines[:insertAt]...)
		next = append(next, key+": "+values[key])
		next = append(next, lines[insertAt:]...)
		lines = next
		found[key] = true
	}
	return "---\n" + strings.Join(lines, "\n") + body, nil
}

func (h *Hub) activeSpecs(ticketID string) ([]string, error) {
	dir := filepath.Join(h.Root, filepath.FromSlash(h.specDirRel()))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, &ToolError{Code: "ERR_GIT", Detail: err.Error()}
	}
	var out []string
	prefix := ""
	if ticketID != "" {
		prefix = "SPEC - [" + ticketID + "]"
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasPrefix(name, "SPEC - [") || !strings.HasSuffix(name, ".md") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		out = append(out, h.specDirRel()+"/"+name)
	}
	sort.Strings(out)
	return out, nil
}

func (h *Hub) trackedDirty(ctx context.Context) ([]string, error) {
	stdout, err := h.gitLocalOut(ctx, "status", "--porcelain=v1", "-z")
	if err != nil {
		return nil, err
	}
	return parsePorcelainZ(stdout), nil
}

func parsePorcelainZ(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, "\x00")
	var out []string
	for i := 0; i < len(parts); i++ {
		rec := parts[i]
		if rec == "" {
			continue
		}
		if len(rec) < 4 {
			continue
		}
		xy := rec[:2]
		path := rec[3:]
		if xy == "??" || xy == "!!" {
			continue
		}
		if xy[0] == 'R' || xy[0] == 'C' || xy[1] == 'R' || xy[1] == 'C' {
			i++
			if i < len(parts) {
				path = parts[i]
			}
		}
		out = append(out, filepath.ToSlash(path))
	}
	if out == nil {
		return []string{}
	}
	return out
}

func (h *Hub) branchExists(ctx context.Context, name string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, localGitTimeout)
	defer cancel()
	_, _, err := h.Run.Run(ctx, h.Root, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false, &ToolError{Code: "ERR_TIMEOUT"}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, execErr(err, "")
}

func (h *Hub) gitLocal(ctx context.Context, args ...string) error {
	_, err := h.gitLocalOut(ctx, args...)
	return err
}

func (h *Hub) gitLocalOut(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, localGitTimeout)
	defer cancel()
	stdout, stderr, err := h.Run.Run(ctx, h.Root, "git", args...)
	if err != nil {
		return "", execErr(err, stderr)
	}
	return stdout, nil
}

func (h *Hub) gitSync(ctx context.Context, args ...string) error {
	_, err := h.gitSyncOut(ctx, args...)
	return err
}

func (h *Hub) gitSyncOut(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	stdout, stderr, err := h.Run.Run(ctx, h.Root, "git", args...)
	if err != nil {
		return "", execErr(err, stderr)
	}
	return stdout, nil
}

// todoHold keeps a dirty TODO.md outside the global git stash stack.
type todoHold struct {
	bakPath  string
	basePath string
	active   bool
}

func (h *Hub) holdTodo(ctx context.Context) (*todoHold, error) {
	rel := h.todoRel()
	work := filepath.Join(h.Root, filepath.FromSlash(rel))
	body, err := os.ReadFile(work)
	if err != nil {
		return nil, &ToolError{Code: "ERR_STASH_FAILED", Detail: err.Error()}
	}
	baseRaw, err := h.gitLocalOut(ctx, "show", "HEAD:"+rel)
	if err != nil {
		return nil, &ToolError{Code: "ERR_STASH_FAILED", Detail: err.Error()}
	}
	bakPath, err := h.gitPath(ctx, "mcp-todo.bak")
	if err != nil {
		return nil, err
	}
	basePath, err := h.gitPath(ctx, "mcp-todo.bak.base")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(bakPath, body); err != nil {
		return nil, &ToolError{Code: "ERR_STASH_FAILED", Detail: err.Error()}
	}
	if err := writeAtomic(basePath, []byte(baseRaw)); err != nil {
		_ = os.Remove(bakPath)
		return nil, &ToolError{Code: "ERR_STASH_FAILED", Detail: err.Error()}
	}
	if err := h.gitLocal(ctx, "checkout", "--", rel); err != nil {
		_ = os.Remove(bakPath)
		_ = os.Remove(basePath)
		return nil, err
	}
	return &todoHold{bakPath: bakPath, basePath: basePath, active: true}, nil
}

func (h *Hub) releaseTodo(ctx context.Context, hold *todoHold) error {
	if hold == nil || !hold.active {
		return nil
	}
	hold.active = false
	work := filepath.Join(h.Root, filepath.FromSlash(h.todoRel()))
	mergeCtx, cancel := context.WithTimeout(ctx, localGitTimeout)
	defer cancel()
	_, stderr, err := h.Run.Run(mergeCtx, h.Root, "git", "merge-file", "-q", work, hold.basePath, hold.bakPath)
	_ = os.Remove(hold.bakPath)
	_ = os.Remove(hold.basePath)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ToolError{Code: "ERR_TIMEOUT"}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return &ToolError{Code: "ERR_STASH_FAILED", Detail: "todo restore conflict"}
	}
	return execErr(err, stderr)
}

func (h *Hub) gitPath(ctx context.Context, name string) (string, error) {
	out, err := h.gitLocalOut(ctx, "rev-parse", "--git-path", name)
	if err != nil {
		return "", &ToolError{Code: "ERR_STASH_FAILED", Detail: err.Error()}
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return "", &ToolError{Code: "ERR_STASH_FAILED", Detail: "git path"}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(h.Root, p)
	}
	return p, nil
}

func writeAtomic(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-todo-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func execErr(err error, stderr string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &ToolError{Code: "ERR_TIMEOUT"}
	}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	return &ToolError{Code: "ERR_GIT", Detail: msg}
}

func asTool(err error) error {
	var pathErr *guard.PathTokenError
	if errors.As(err, &pathErr) {
		return &ToolError{Code: "ERR_INVALID_PATH", Detail: pathErr.Field + " " + pathErr.Reason}
	}
	return err
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func relSlash(root, full string) string {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return filepath.ToSlash(full)
	}
	return filepath.ToSlash(rel)
}
