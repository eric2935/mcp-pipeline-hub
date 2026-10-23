package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigMatchesIEMSTickets(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ToolName != "iems-mcp-hub" || cfg.GetSpecActiveDir() != "obsidian-docs/v2/03_Spec (구현명세서)" {
		t.Fatalf("%s %s", cfg.ToolName, cfg.GetSpecActiveDir())
	}
	if cfg.GetSpecArchiveDir() != "obsidian-docs/v2/03_Spec (구현명세서)/_archive" {
		t.Fatal(cfg.GetSpecArchiveDir())
	}
	for _, id := range []string{"IEMS-451", "IEMS-401-2", "IEMS-203-10"} {
		if !cfg.MatchTicket(id) {
			t.Fatalf("expected match %s", id)
		}
	}
	for _, id := range []string{"IEMS-451-go", "SHOP-1", "iems-451"} {
		if cfg.MatchTicket(id) {
			t.Fatalf("expected reject %s", id)
		}
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

func TestWorkspaceSampleMatchesDefault(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, FileName)); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(root, "")
	if err != nil {
		t.Fatal(err)
	}
	def := DefaultConfig()
	if cfg.ToolName != def.ToolName || cfg.TicketPrefix != def.TicketPrefix || cfg.TicketPattern != def.TicketPattern {
		t.Fatalf("sample %#v", cfg)
	}
	if cfg.GetSpecActiveDir() != def.GetSpecActiveDir() || cfg.GetSpecArchiveDir() != def.GetSpecArchiveDir() || cfg.TodoFile != def.TodoFile {
		t.Fatalf("paths %s %s", cfg.GetSpecActiveDir(), cfg.TodoFile)
	}
	if strings.Join(cfg.SSOTManifests, "\n") != strings.Join(def.SSOTManifests, "\n") {
		t.Fatalf("ssot %#v", cfg.SSOTManifests)
	}
	if strings.Join(cfg.DirtyBlacklist, "\n") != strings.Join(def.DirtyBlacklist, "\n") {
		t.Fatalf("dirty %#v", cfg.DirtyBlacklist)
	}
	if strings.Join(cfg.CloseExcludePaths, "\n") != strings.Join(def.CloseExcludePaths, "\n") {
		t.Fatalf("close %#v", cfg.CloseExcludePaths)
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
		if strings.Contains(path, "02_labs") || strings.Contains(path, "03_tools") {
			t.Fatalf("domain path %s", path)
		}
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
