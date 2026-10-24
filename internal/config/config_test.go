package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigIsNeutral(t *testing.T) {
	cfg := DefaultConfig()
	want, err := InitTemplate("PROJ", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolName != "agentic-mcp-hub" || cfg.TicketPrefix != "PROJ" || cfg.TicketPattern != want.TicketPattern {
		t.Fatalf("%+v", cfg)
	}
	if cfg.GetSpecActiveDir() != "obsidian-docs/v2/03_Spec (구현명세서)" {
		t.Fatal(cfg.GetSpecActiveDir())
	}
	if cfg.GetSpecArchiveDir() != "obsidian-docs/v2/03_Spec (구현명세서)/_archive" {
		t.Fatal(cfg.GetSpecArchiveDir())
	}
	if len(cfg.SSOTManifests) != 0 {
		t.Fatalf("ssot %#v", cfg.SSOTManifests)
	}
	if !cfg.MatchTicket("PROJ-1") || !cfg.MatchTicket("PROJ-401-2") || cfg.MatchTicket("IEMS-451") {
		t.Fatal("ticket match")
	}
	joined := strings.Join(append(append([]string{}, cfg.DirtyBlacklist...), cfg.CloseExcludePaths...), "\n")
	if strings.Contains(joined, "03_tools") || strings.Contains(joined, "02_labs") || !strings.Contains(joined, "obsidian-docs/v2/") {
		t.Fatal(joined)
	}
}

func TestLoadConfigFallsBackWhenFileMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolName != DefaultConfig().ToolName || cfg.TicketPattern != DefaultConfig().TicketPattern {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadConfigExplicitMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadConfig(dir, filepath.Join(dir, "missing.json"))
	if err == nil || !strings.Contains(err.Error(), ErrNotFound) {
		t.Fatal(err)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(dir, "")
	if err == nil || !strings.Contains(err.Error(), ErrInvalid) {
		t.Fatal(err)
	}
}

func TestLoadConfigRejectsUnanchoredPattern(t *testing.T) {
	dir := t.TempDir()
	body := `{
	  "tool_name": "shop-hub",
	  "ticket_prefix": "SHOP",
	  "ticket_pattern": "SHOP-[0-9]+",
	  "vault_dir": "docs",
	  "spec_dir": "specs",
	  "spec_archive_dir": "_archive",
	  "todo_file": "TODO.md"
	}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(dir, "")
	if err == nil || !strings.Contains(err.Error(), ErrInvalid) {
		t.Fatal(err)
	}
}

func TestLoadConfigUsesVault(t *testing.T) {
	dir := t.TempDir()
	body := `{
	  "tool_name": "shop-hub",
	  "ticket_prefix": "SHOP",
	  "ticket_pattern": "^SHOP-[0-9]+(?:-[0-9]+)?$",
	  "vault_dir": "shop-docs",
	  "spec_dir": "specs",
	  "spec_archive_dir": "old",
	  "todo_file": "BOARD.md"
	}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetSpecActiveDir() != "shop-docs/specs" || cfg.TodoFile != "BOARD.md" {
		t.Fatalf("%s %s", cfg.GetSpecActiveDir(), cfg.TodoFile)
	}
	if !cfg.MatchTicket("SHOP-12-3") || cfg.MatchTicket("IEMS-12") {
		t.Fatal("ticket match")
	}
}

func TestDefaultConfigMatchesInitTemplate(t *testing.T) {
	def := DefaultConfig()
	want, err := InitTemplate("PROJ", "obsidian-docs/v2")
	if err != nil {
		t.Fatal(err)
	}
	if def.ToolName != want.ToolName || def.TicketPrefix != want.TicketPrefix || def.TicketPattern != want.TicketPattern {
		t.Fatalf("%#v", def)
	}
	if def.GetSpecActiveDir() != want.GetSpecActiveDir() || def.TodoFile != want.TodoFile {
		t.Fatalf("%s %s", def.GetSpecActiveDir(), def.TodoFile)
	}
	if strings.Join(def.SSOTManifests, "\n") != strings.Join(want.SSOTManifests, "\n") {
		t.Fatalf("ssot %#v", def.SSOTManifests)
	}
	if strings.Join(def.DirtyBlacklist, "\n") != strings.Join(want.DirtyBlacklist, "\n") {
		t.Fatalf("dirty %#v", def.DirtyBlacklist)
	}
	if strings.Join(def.CloseExcludePaths, "\n") != strings.Join(want.CloseExcludePaths, "\n") {
		t.Fatalf("close %#v", def.CloseExcludePaths)
	}
}

func TestInitTemplateUsesPrefix(t *testing.T) {
	cfg, err := InitTemplate("SHOP", "test-docs")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolName != "agentic-mcp-hub" || cfg.TicketPrefix != "SHOP" || cfg.VaultDir != "test-docs" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.TicketPattern != `^SHOP-[0-9]+(?:-[0-9]+)?$` || !cfg.MatchTicket("SHOP-1") {
		t.Fatal(cfg.TicketPattern)
	}
	mixed, err := InitTemplate("iCMS", "docs")
	if err != nil {
		t.Fatal(err)
	}
	if mixed.TicketPrefix != "iCMS" || mixed.TicketPattern != `^iCMS-[0-9]+(?:-[0-9]+)?$` || !mixed.MatchTicket("iCMS-1") {
		t.Fatalf("%+v", mixed)
	}
	if len(cfg.SSOTManifests) != 0 {
		t.Fatalf("ssot %#v", cfg.SSOTManifests)
	}
	for _, path := range append(append([]string{}, cfg.DirtyBlacklist...), cfg.CloseExcludePaths...) {
		if strings.Contains(path, "02_labs") || strings.Contains(path, "03_tools") || strings.Contains(path, "obsidian-docs") {
			t.Fatalf("domain path %s", path)
		}
	}
	if !containsString(cfg.DirtyBlacklist, "test-docs/") || !containsString(cfg.CloseExcludePaths, "test-docs/") {
		t.Fatalf("blacklist %#v close %#v", cfg.DirtyBlacklist, cfg.CloseExcludePaths)
	}
	for _, prefix := range []string{"shop", "my-app", "APP", "CORE"} {
		got, err := InitTemplate(prefix, "docs")
		if err != nil {
			t.Fatal(err)
		}
		if got.TicketPrefix != prefix || !got.MatchTicket(prefix+"-1") {
			t.Fatalf("%+v", got)
		}
	}
	for _, prefix := range []string{"", "-app", "---", "9CMS"} {
		if _, err := InitTemplate(prefix, "docs"); err == nil {
			t.Fatalf("prefix %q must fail", prefix)
		}
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
