package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseTicketFlagSkipsServerRoot(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := run([]string{"-root", dir, "-close-ticket"}, strings.NewReader("\n"), &bytes.Buffer{}, &stderr)
	if code == 2 || strings.Contains(stderr.String(), "-root is required") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if code == 0 {
		t.Fatal("empty dir must fail")
	}
}

func TestCloseTicketExclusive(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"-ingest-clipboard", "-close-ticket"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func TestServerRequiresRoot(t *testing.T) {
	var stderr bytes.Buffer
	code := run(nil, bytes.NewReader(nil), &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "-root is required") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func TestInitFlagCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", dir, "-init", "-prefix", "TEST", "-vault", "test-docs"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp-pipeline.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test-docs", "03_Spec (구현명세서)", "_archive")); err != nil {
		t.Fatal(err)
	}
}

func TestInitFlagDefaultPrefixProjectAndForce(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", dir, "-init", "-project", "DemoProject"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "[CREATE] .mcp-pipeline.json") {
		t.Fatal(stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ticket_prefix": "PROJ"`) {
		t.Fatal(string(raw))
	}
	manual, err := os.ReadFile(filepath.Join(dir, "obsidian-docs", "v2", "01_SOP (표준운영절차)", "표준-업무-매뉴얼.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manual), "DemoProject") || !strings.Contains(string(manual), "PROJ") {
		t.Fatal(string(manual))
	}
	stderr.Reset()
	code = run([]string{"-root", dir, "-init"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.Contains(stderr.String(), "[SKIP] .mcp-pipeline.json") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	code = run([]string{"-root", dir, "-init", "-force", "-project", "Renamed"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.Contains(stderr.String(), "[OVERWRITE] .mcp-pipeline.json") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	manual, err = os.ReadFile(filepath.Join(dir, "obsidian-docs", "v2", "01_SOP (표준운영절차)", "표준-업무-매뉴얼.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manual), "Renamed") {
		t.Fatal(string(manual))
	}
}

func TestInitFlagsRequireInit(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"-project", "X"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "require -init") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func TestInitFlagAcceptsMixedCasePrefix(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", dir, "-init", "-prefix", "iCMS"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp-pipeline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"ticket_prefix": "iCMS"`) {
		t.Fatal(string(raw))
	}
}

func TestInvalidConfigExits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp-pipeline.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"-root", dir, "-close-ticket"}, strings.NewReader("\n"), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "ERR_CONFIG_INVALID") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func TestExplicitConfigMissing(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	missing := filepath.Join(dir, "missing.json")
	code := run([]string{"-root", dir, "-config", missing, "-close-ticket"}, strings.NewReader("\n"), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "ERR_CONFIG_NOT_FOUND") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}
