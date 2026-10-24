package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSubstitutesAndOmitsDomainTerms(t *testing.T) {
	ctx := Context{
		ProjectName:  "DemoProject",
		TicketPrefix: "DEMO",
		CreatedAt:    "2026-10-24",
		TargetRoot:   "/tmp/pipeline-demo",
		VaultDir:     "obsidian-docs/v2",
	}
	files, err := Render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manual := string(files["obsidian-docs/v2/01_SOP (표준운영절차)/표준-업무-매뉴얼.md"])
	taxonomy := string(files["obsidian-docs/v2/01_SOP (표준운영절차)/SOP - 지식창고 분류 체계 (Taxonomy).md"])
	glossary := string(files["obsidian-docs/v2/06_Cheatsheet/GLS - Project Glossary.md"])
	for _, body := range []string{manual, taxonomy, glossary} {
		for _, want := range []string{"DemoProject", "DEMO", "2026-10-24"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing %s\n%s", want, body)
			}
		}
		for _, banned := range []string{"3축 진동", "Modbus", "TimescaleDB"} {
			if strings.Contains(body, banned) {
				t.Fatalf("banned %s", banned)
			}
		}
	}
	for _, term := range []string{
		"항상 성립 조건 (Invariant)",
		"재실행 안전 (Idempotent)",
		"통째 처리 (Atomicity)",
		"확정 동작 (Deterministic)",
		"대체 경로 (Fallback)",
		"역할과 책임 (Roles and Responsibilities)",
		"상류 적체 (Backpressure)",
		"조기 실패 (Fail-Fast)",
		"고장 시 안전 (Fail-Safe)",
		"독립 분리 (Decoupling)",
		"상태 채움 (Hydration)",
		"동기화 대기선 (Barrier)",
		"세대 차단 (Generation Barrier)",
	} {
		if !strings.Contains(glossary, term) {
			t.Fatalf("missing term %s", term)
		}
	}
	for _, category := range []string{"architecture", "spec", "tech_note", "cheatsheet", "log"} {
		if !strings.Contains(taxonomy, category) {
			t.Fatalf("missing category %s", category)
		}
	}
	cfg := string(files[".mcp-pipeline.json"])
	if strings.Contains(cfg, "02_labs") || strings.Contains(cfg, "03_tools") || !strings.Contains(cfg, `"ssot_manifests": []`) {
		t.Fatal(cfg)
	}
	if !strings.Contains(cfg, `"ticket_prefix": "DEMO"`) || !strings.Contains(cfg, `^DEMO-[0-9]+(?:-[0-9]+)?$`) {
		t.Fatal(cfg)
	}
	mcp := string(files[".cursor/mcp.json"])
	if !strings.Contains(mcp, `"command": "mcp-hub"`) || !strings.Contains(mcp, `"-root"`) || !strings.Contains(mcp, "/tmp/pipeline-demo") {
		t.Fatal(mcp)
	}
	if strings.Contains(mcp, "03_tools/mcp-hub/bin") {
		t.Fatal(mcp)
	}
}

func TestRenderPreservesPrefixCase(t *testing.T) {
	files, err := Render(Context{
		ProjectName:  "Case",
		TicketPrefix: "iCMS",
		CreatedAt:    "2026-10-24",
		TargetRoot:   "/work",
		VaultDir:     "docs",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(files[".mcp-pipeline.json"])
	if !strings.Contains(cfg, `"ticket_prefix": "iCMS"`) || !strings.Contains(cfg, `"docs/"`) || strings.Contains(cfg, "obsidian-docs") {
		t.Fatal(cfg)
	}
	manual := string(files["docs/01_SOP (표준운영절차)/표준-업무-매뉴얼.md"])
	if !strings.Contains(manual, "hub_prepare_branch") || strings.Contains(manual, "obsidian-docs") || strings.Contains(manual, "iems_prepare_branch") {
		t.Fatal(manual)
	}
}

func TestRenderGovernancePack(t *testing.T) {
	files, err := Render(Context{
		ProjectName:  "Sample",
		TicketPrefix: "DEMO",
		CreatedAt:    "2026-10-24",
		TargetRoot:   "/tmp/pipeline-demo",
		VaultDir:     "my-notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	rules := string(files[".cursorrules"])
	if strings.Contains(strings.ToLower(rules), "iems") {
		t.Fatal(rules)
	}
	for _, want := range []string{
		"Sample",
		"DEMO",
		"my-notes",
		"feature/DEMO-XXX",
		"hub_pack_context",
		"hub_ingest_spec",
		"hub_prepare_branch",
		"hub_inspect_pipeline",
		"hub_finalize_spec",
		"%%{init: {'theme': 'neutral', 'themeVariables': {'background': 'transparent', 'clusterBkg': 'none', 'clusterBorder': '#7f8c8d'}}}%%",
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("missing %s\n%s", want, rules)
		}
	}
	ignore := string(files[".cursorignore"])
	if !strings.Contains(ignore, "# my-notes/ 는 Cursor가 읽도록 색인에 남긴다.") {
		t.Fatal(ignore)
	}
	for _, line := range strings.Split(ignore, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.Contains(trim, "my-notes") {
			t.Fatalf("vault pattern %q", line)
		}
	}
	for _, pattern := range []string{"repomix-output.*", "node_modules/", "dist/", "bin/", "vendor/", "*.pdf", ".git/"} {
		if !strings.Contains(ignore, pattern) {
			t.Fatalf("missing %s\n%s", pattern, ignore)
		}
	}
	var pack struct {
		Output struct {
			FilePath   string `json:"filePath"`
			HeaderText string `json:"headerText"`
		} `json:"output"`
		Include []string `json:"include"`
	}
	if err := json.Unmarshal(files["repomix.config.json"], &pack); err != nil {
		t.Fatal(err)
	}
	if pack.Output.FilePath != "repomix-output.xml" || pack.Output.HeaderText != "Sample" {
		t.Fatalf("%#v", pack)
	}
	if len(pack.Include) != 1 || pack.Include[0] != "**/*" {
		t.Fatalf("%#v", pack.Include)
	}
	repomixIgnore := string(files[".repomixignore"])
	for _, pattern := range []string{
		"**/*_test.*",
		"**/*.spec.*",
		"**/_archive/**",
		"my-notes/05_Log/**",
		"TODO_ARCHIVE.md",
	} {
		if !strings.Contains(repomixIgnore, pattern) {
			t.Fatalf("missing %s\n%s", pattern, repomixIgnore)
		}
	}

	quoted, err := Render(Context{
		ProjectName:  `Qa "Hub"`,
		TicketPrefix: "DEMO",
		CreatedAt:    "2026-10-24",
		TargetRoot:   "/tmp/pipeline-demo",
		VaultDir:     "my-notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(quoted["repomix.config.json"], &pack); err != nil {
		t.Fatal(err)
	}
	if pack.Output.HeaderText != `Qa "Hub"` {
		t.Fatal(pack.Output.HeaderText)
	}
}

func TestRenderRejectsBadPrefixWithoutFiles(t *testing.T) {
	_, err := Render(Context{ProjectName: "Demo", TicketPrefix: "-app", TargetRoot: "/tmp", CreatedAt: "2026-10-24"})
	if err == nil || !strings.Contains(err.Error(), "ERR_CONFIG_INVALID") {
		t.Fatal(err)
	}
}

func TestWriteSkipsThenForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	files, err := Render(Context{
		ProjectName:  "DemoProject",
		TicketPrefix: "DEMO",
		CreatedAt:    "2026-10-24",
		TargetRoot:   dir,
		VaultDir:     "vault",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := Write(dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Created) != len(files) || len(first.Skipped) != 0 || len(first.Overwritten) != 0 {
		t.Fatalf("%#v", first)
	}
	manual := filepath.Join(dir, "vault", "01_SOP (표준운영절차)", "표준-업무-매뉴얼.md")
	before, err := os.ReadFile(manual)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Write(dir, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Created) != 0 || len(second.Overwritten) != 0 || len(second.Skipped) != len(files) {
		t.Fatalf("%#v", second)
	}
	after, err := os.ReadFile(manual)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("skip changed the manual")
	}
	files, err = Render(Context{
		ProjectName:  "Other",
		TicketPrefix: "DEMO",
		CreatedAt:    "2026-10-24",
		TargetRoot:   dir,
		VaultDir:     "vault",
	})
	if err != nil {
		t.Fatal(err)
	}
	third, err := Write(dir, files, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Overwritten) != len(files) || len(third.Created) != 0 {
		t.Fatalf("%#v", third)
	}
	replaced, err := os.ReadFile(manual)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(replaced), "Other") || string(replaced) == string(before) {
		t.Fatal(string(replaced))
	}
}
