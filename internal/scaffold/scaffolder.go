package scaffold

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"iems/mcp-hub/internal/config"
)

const (
	configRel = ".mcp-pipeline.json"
	todoRel   = "TODO.md"
	cursorRel = ".cursor/mcp.json"
)

// Context is the text/template data for one -init run.
type Context struct {
	ProjectName   string
	TicketPrefix  string
	TicketPattern string
	CreatedAt     string
	TargetRoot    string
	VaultDir      string
}

// Report lists scaffold files created, left in place, or replaced.
type Report struct {
	Created     []string
	Skipped     []string
	Overwritten []string
}

// Render parses every embedded template before returning file bodies.
// Paths are slash-separated and relative to the workspace root.
func Render(ctx Context) (map[string][]byte, error) {
	if strings.TrimSpace(ctx.ProjectName) == "" {
		return nil, fmt.Errorf("project name")
	}
	if strings.TrimSpace(ctx.TargetRoot) == "" {
		return nil, fmt.Errorf("target root")
	}
	if ctx.CreatedAt == "" {
		ctx.CreatedAt = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", ctx.CreatedAt); err != nil {
		return nil, fmt.Errorf("created_at")
	}
	want, err := config.InitTemplate(ctx.TicketPrefix, ctx.VaultDir)
	if err != nil {
		return nil, err
	}
	ctx.TicketPrefix = want.TicketPrefix
	ctx.TicketPattern = want.TicketPattern
	ctx.VaultDir = want.VaultDir

	tmpl, err := template.New("scaffold").Funcs(template.FuncMap{
		"jsonQuote": jsonQuote,
	}).Option("missingkey=error").ParseFS(Templates, "templates/*")
	if err != nil {
		return nil, err
	}
	rels := map[string]string{
		"config.json.tmpl":     configRel,
		"todo.md.tmpl":         todoRel,
		"cursor_mcp.json.tmpl": cursorRel,
		"manual.md.tmpl":       vaultFile(ctx.VaultDir, "01_SOP (표준운영절차)", "표준-업무-매뉴얼.md"),
		"taxonomy.md.tmpl":     vaultFile(ctx.VaultDir, "01_SOP (표준운영절차)", "SOP - 지식창고 분류 체계 (Taxonomy).md"),
		"glossary.md.tmpl":     vaultFile(ctx.VaultDir, "06_Cheatsheet", "GLS - Project Glossary.md"),
	}
	out := make(map[string][]byte, len(rels))
	names := make([]string, 0, len(rels))
	for name := range rels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := execute(tmpl, name, ctx)
		if err != nil {
			return nil, err
		}
		out[rels[name]] = body
	}
	var got config.PipelineConfig
	if err := json.Unmarshal(out[configRel], &got); err != nil {
		return nil, fmt.Errorf("config template: %w", err)
	}
	if err := got.Validate(); err != nil {
		return nil, err
	}
	if !sameConfig(&got, want) {
		return nil, fmt.Errorf("config template does not match InitTemplate")
	}
	return out, nil
}

// Write creates missing files. Existing files are kept unless force is set.
func Write(root string, files map[string][]byte, force bool) (Report, error) {
	var report Report
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if err := writeOne(root, rel, files[rel], force, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func writeOne(root, rel string, body []byte, force bool, report *Report) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	_, err := os.Stat(full)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists && !force {
		report.Skipped = append(report.Skipped, filepath.ToSlash(rel))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, body, 0o644); err != nil {
		return err
	}
	slash := filepath.ToSlash(rel)
	if exists {
		report.Overwritten = append(report.Overwritten, slash)
		return nil
	}
	report.Created = append(report.Created, slash)
	return nil
}

func execute(tmpl *template.Template, name string, ctx Context) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, ctx); err != nil {
		return nil, err
	}
	body := buf.Bytes()
	if !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	return body, nil
}

func vaultFile(vault string, elem ...string) string {
	parts := append([]string{vault}, elem...)
	return filepath.ToSlash(filepath.Join(parts...))
}

func jsonQuote(s string) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sameConfig(got, want *config.PipelineConfig) bool {
	if got == nil || want == nil {
		return false
	}
	return got.ToolName == want.ToolName &&
		got.TicketPrefix == want.TicketPrefix &&
		got.TicketPattern == want.TicketPattern &&
		got.VaultDir == want.VaultDir &&
		got.SpecDir == want.SpecDir &&
		got.SpecArchiveDir == want.SpecArchiveDir &&
		got.TodoFile == want.TodoFile &&
		slices.Equal(got.SSOTManifests, want.SSOTManifests) &&
		slices.Equal(got.DirtyBlacklist, want.DirtyBlacklist) &&
		slices.Equal(got.CloseExcludePaths, want.CloseExcludePaths)
}
