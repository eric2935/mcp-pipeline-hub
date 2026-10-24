package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mcp-pipeline-hub/internal/config"
	"mcp-pipeline-hub/internal/pipeline"
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
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", dir, "-close-ticket"}, strings.NewReader("\n"), &stdout, &stderr)
	if code != 1 || stderr.String() != "ERR_CONFIG_INVALID: unexpected end of JSON input\n" {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatal(stdout.String())
	}
}

func TestExplicitConfigMissing(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(dir, "missing.json")
	code := run([]string{"-root", dir, "-config", missing, "-close-ticket"}, strings.NewReader("\n"), &stdout, &stderr)
	if code != 1 || stderr.String() != "ERR_CONFIG_NOT_FOUND: "+missing+"\n" {
		t.Fatalf("%d %s", code, stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "[DEBUG]") {
		t.Fatalf("stdout %q stderr %s", stdout.String(), stderr.String())
	}
}

func TestMissingConfigDebugFlag(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(dir, "nonexistent.json")
	code := run([]string{"-config", missing, "-debug"}, strings.NewReader(""), &stdout, &stderr)
	want := "ERR_CONFIG_NOT_FOUND: " + missing + "\n" +
		"[DEBUG] error type: *config.Error\n" +
		"[DEBUG] unwrap: <none>\n" +
		"[DEBUG] context: code=ERR_CONFIG_NOT_FOUND detail=" + missing + "\n"
	if code != 1 || stderr.String() != want {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatal(stdout.String())
	}
}

func TestMissingConfigDebugEnv(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nonexistent.json")
	for _, value := range []string{"1", "true", "TRUE"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MCP_DEBUG", value)
			var stdout, stderr bytes.Buffer
			code := run([]string{"-config", missing}, strings.NewReader(""), &stdout, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), "[DEBUG] error type: *config.Error") {
				t.Fatalf("%d %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "[DEBUG] unwrap: <none>") || stdout.Len() != 0 {
				t.Fatalf("stdout %q stderr %s", stdout.String(), stderr.String())
			}
		})
	}
}

func TestDebugEnvOff(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	for _, value := range []string{"0", "false", "yes"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MCP_DEBUG", value)
			var stdout, stderr bytes.Buffer
			code := run([]string{"-config", missing}, strings.NewReader(""), &stdout, &stderr)
			if code != 1 || strings.Contains(stderr.String(), "[DEBUG]") || stdout.Len() != 0 {
				t.Fatalf("%d stdout %q stderr %s", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestDebugEnvStaysOnWhenFlagFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MCP_DEBUG", "1")
	var stderr bytes.Buffer
	missing := filepath.Join(dir, "missing.json")
	code := run([]string{"-config", missing, "-debug=false"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "[DEBUG] error type:") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func TestDebugFlagDoesNotCountAsMode(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"-debug", "-ingest-clipboard", "-close-ticket"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	dir := t.TempDir()
	stderr.Reset()
	var stdout bytes.Buffer
	code = run([]string{"-root", dir, "-debug", "-close-ticket"}, strings.NewReader("\n"), &stdout, &stderr)
	if strings.Contains(stderr.String(), "mutually exclusive") || stdout.Len() != 0 {
		t.Fatalf("%d stdout %q stderr %s", code, stdout.String(), stderr.String())
	}
}

func TestWriteCLIErrorDetailFormatting(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "tool detail",
			err:  &pipeline.ToolError{Code: "ERR_GIT", Detail: "origin/main"},
			want: "ERR_GIT: origin/main\n",
		},
		{
			name: "empty detail",
			err:  &pipeline.ToolError{Code: "ERR_TIMEOUT"},
			want: "ERR_TIMEOUT\n",
		},
		{
			name: "blank detail",
			err:  &config.Error{Code: "ERR_CONFIG_INVALID", Detail: " \n\t "},
			want: "ERR_CONFIG_INVALID\n",
		},
		{
			name: "multiline detail",
			err:  &config.Error{Code: "ERR_CONFIG_INVALID", Detail: "line1\nline2"},
			want: "ERR_CONFIG_INVALID: line1 line2\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			writeCLIError(&stderr, tc.err, false)
			if stderr.String() != tc.want {
				t.Fatalf("%q", stderr.String())
			}
		})
	}
}

func TestIssueTokenFlag(t *testing.T) {
	dir := t.TempDir()
	execGit(t, dir, "init", "-b", "main")
	prev := copyToClipboard
	t.Cleanup(func() { copyToClipboard = prev })
	var copied string
	copyFailed := false
	copyToClipboard = func(ctx context.Context, text string) error {
		if copyFailed {
			return errors.New("clipboard down")
		}
		copied = text
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-root", dir, "-issue-token", "PROJ-3"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	token := strings.TrimSpace(stdout.String())
	if token != copied || !strings.HasPrefix(token, "tok_") || strings.Contains(stdout.String(), "\n\n") {
		t.Fatalf("stdout %q copied %q stderr %s", stdout.String(), copied, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatal(stderr.String())
	}
	body, err := os.ReadFile(filepath.Join(dir, ".git", "mcp-finalize-tokens", "PROJ-3.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) || !strings.Contains(string(body), "token_hash") {
		t.Fatalf("%s", body)
	}

	stdout.Reset()
	stderr.Reset()
	copyFailed = true
	code = run([]string{"-root", dir, "-token", "PROJ-3"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(stdout.String()), "tok_") || !strings.Contains(stderr.String(), "WARN: clipboard copy failed") {
		t.Fatalf("%d stdout %q stderr %s", code, stdout.String(), stderr.String())
	}

	stderr.Reset()
	code = run([]string{"-issue-token", "PROJ-3", "-token", "PROJ-4"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "values differ") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	code = run([]string{"-issue-token", "PROJ-3", "-close-ticket"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "mutually exclusive") || !strings.Contains(stderr.String(), "-issue-token") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	code = run([]string{"-root", dir, "-issue-token", "nope"}, strings.NewReader(""), &bytes.Buffer{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "ERR_INVALID_PATH") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

func execGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCLIErrorDebugDomainAndUnwrap(t *testing.T) {
	var stderr bytes.Buffer
	writeCLIError(&stderr, &pipeline.ToolError{Code: "ERR_TIMEOUT"}, true)
	want := "ERR_TIMEOUT\n[DEBUG] error type: *pipeline.ToolError\n[DEBUG] unwrap: <none>\n[DEBUG] context: code=ERR_TIMEOUT detail=\n"
	if stderr.String() != want {
		t.Fatalf("%q", stderr.String())
	}
	stderr.Reset()
	err := fmt.Errorf("outer: %w", errors.New("root-cause"))
	writeCLIError(&stderr, err, true)
	got := stderr.String()
	if !strings.HasPrefix(got, "outer: root-cause\n") || !strings.Contains(got, "[DEBUG] unwrap: root-cause\n") || !strings.Contains(got, "[DEBUG] context: message=outer: root-cause\n") {
		t.Fatal(got)
	}
}
