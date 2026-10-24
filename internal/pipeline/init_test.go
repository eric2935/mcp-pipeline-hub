package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesSkeletonAndSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	opt := InitOptions{Prefix: "SHOP", Vault: "test-docs", ProjectName: "Shop", CreatedAt: "2026-10-24"}
	report, err := RunInitProject(dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".mcp-pipeline.json", "TODO.md", ".cursor/mcp.json", ".gitignore", ".cursorrules", ".cursorignore", "repomix.config.json", ".repomixignore"} {
		if !containsString(report.Created, name) {
			t.Fatalf("created %#v missing %s", report.Created, name)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"tool_name": "agentic-mcp-hub"`) || !strings.Contains(text, `"ticket_prefix": "SHOP"`) {
		t.Fatal(text)
	}
	if !strings.Contains(text, `"vault_dir": "test-docs"`) || !strings.Contains(text, `^SHOP-[0-9]+(?:-[0-9]+)?$`) {
		t.Fatal(text)
	}
	mcp, err := os.ReadFile(filepath.Join(dir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(dir)
	mcpText := string(mcp)
	if !strings.Contains(mcpText, `"command": "mcp-hub"`) || !strings.Contains(mcpText, abs) || !strings.Contains(mcpText, "agentic-mcp-hub") {
		t.Fatal(mcpText)
	}
	if strings.Contains(mcpText, "/opt/mcp-hub") {
		t.Fatal(mcpText)
	}
	todo, err := os.ReadFile(filepath.Join(dir, "TODO.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"## In Progress", "## Backlog", "## Done", "착수 승인"} {
		if !strings.Contains(string(todo), section) {
			t.Fatal(string(todo))
		}
	}
	archive := filepath.Join(dir, "test-docs", "03_Spec (구현명세서)", "_archive")
	if info, err := os.Stat(archive); err != nil || !info.IsDir() {
		t.Fatal(err)
	}

	before := string(raw)
	again, err := RunInitProject(dir, InitOptions{Prefix: "OTHER", Vault: "test-docs", ProjectName: "Shop", CreatedAt: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Created) != 0 || len(again.Overwritten) != 0 {
		t.Fatalf("second run created %#v overwritten %#v", again.Created, again.Overwritten)
	}
	for _, name := range []string{".cursorrules", ".cursorignore", "repomix.config.json", ".repomixignore"} {
		if !containsString(again.Skipped, name) {
			t.Fatalf("skipped %#v missing %s", again.Skipped, name)
		}
	}
	after, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatal("existing config was overwritten")
	}
}

func TestInitAppendsGitignoreOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\nobsidian-docs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := RunInitProject(dir, InitOptions{Prefix: "SHOP", ProjectName: "Shop", CreatedAt: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(report.Updated, ".gitignore") {
		t.Fatalf("%#v", report)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "obsidian-docs/v2/") || !strings.Contains(text, "repomix-output.xml") || !strings.Contains(text, "bin/") || !strings.Contains(text, ".cursor/") {
		t.Fatal(text)
	}
	if strings.Count(text, "\nobsidian-docs/\n") != 1 {
		t.Fatal(text)
	}
	if !strings.HasPrefix(text, "node_modules/\n") {
		t.Fatal(text)
	}
	again, err := RunInitProject(dir, InitOptions{Prefix: "SHOP", ProjectName: "Shop", CreatedAt: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(again.Skipped, ".gitignore") || containsString(again.Updated, ".gitignore") {
		t.Fatalf("%#v", again)
	}
}

func TestInitAcceptsMixedCasePrefix(t *testing.T) {
	dir := t.TempDir()
	report, err := RunInitProject(dir, InitOptions{Prefix: "iCMS", ProjectName: "Shop", CreatedAt: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(report.Created, ".mcp-pipeline.json") {
		t.Fatalf("%#v", report.Created)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"ticket_prefix": "iCMS"`) || !strings.Contains(text, `^iCMS-[0-9]+(?:-[0-9]+)?$`) {
		t.Fatal(text)
	}
}

func TestInitDefaultsEmptyPrefix(t *testing.T) {
	dir := t.TempDir()
	_, err := RunInitProject(dir, InitOptions{ProjectName: "Demo", CreatedAt: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ticket_prefix": "PROJ"`) {
		t.Fatal(string(raw))
	}
}

func TestInitRejectsPrefix(t *testing.T) {
	for _, prefix := range []string{"-app", "---"} {
		_, err := RunInitProject(t.TempDir(), InitOptions{Prefix: prefix, ProjectName: "Demo"})
		if err == nil || !strings.Contains(err.Error(), "ERR_CONFIG_INVALID") {
			t.Fatalf("prefix %q: %v", prefix, err)
		}
	}
}

func TestInitForceOverwritesAndKeepsGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := InitOptions{Prefix: "SHOP", Vault: "test-docs", ProjectName: "Shop", CreatedAt: "2026-10-24", Force: true}
	report, err := RunInitProject(dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(report.Overwritten, ".mcp-pipeline.json") && !containsString(report.Created, ".mcp-pipeline.json") {
		t.Fatalf("%#v", report)
	}
	if containsString(report.Overwritten, ".gitignore") {
		t.Fatalf("gitignore overwritten %#v", report)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.HasPrefix(text, "node_modules/\n") || !strings.Contains(text, "test-docs/") || strings.Contains(text, "obsidian-docs/") {
		t.Fatal(text)
	}
	manual := filepath.Join(dir, "test-docs", "01_SOP (표준운영절차)", "표준-업무-매뉴얼.md")
	first, err := os.ReadFile(manual)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "Shop") || !strings.Contains(string(first), "SHOP") {
		t.Fatal(string(first))
	}
	opt.ProjectName = "Other"
	again, err := RunInitProject(dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(again.Overwritten, "test-docs/01_SOP (표준운영절차)/표준-업무-매뉴얼.md") {
		t.Fatalf("%#v", again.Overwritten)
	}
	for _, name := range []string{".cursorrules", ".cursorignore", "repomix.config.json", ".repomixignore"} {
		if !containsString(again.Overwritten, name) {
			t.Fatalf("overwritten %#v missing %s", again.Overwritten, name)
		}
	}
	second, err := os.ReadFile(manual)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(second), "Other") || string(second) == string(first) {
		t.Fatal(string(second))
	}
}

func TestInitCustomVaultBindsPaths(t *testing.T) {
	dir := t.TempDir()
	_, err := RunInitProject(dir, InitOptions{Prefix: "DEMO", Vault: "test-vault", ProjectName: "Demo", CreatedAt: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"test-vault/"`) || strings.Contains(string(raw), "obsidian-docs") {
		t.Fatal(string(raw))
	}
	ignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "test-vault/") || strings.Contains(string(ignore), "obsidian-docs") {
		t.Fatal(string(ignore))
	}
	manual, err := os.ReadFile(filepath.Join(dir, "test-vault", "01_SOP (표준운영절차)", "표준-업무-매뉴얼.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(manual)
	if !strings.Contains(text, "hub_prepare_branch") || !strings.Contains(text, "test-vault/") || strings.Contains(text, "iems_prepare_branch") || strings.Contains(text, "obsidian-docs") {
		t.Fatal(text)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
