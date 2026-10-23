package pipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitMessageAndFrontmatterRoundTrip(t *testing.T) {
	msg := commitMessage("feat", "Pipeline", "IEMS-460", `마감: "close"`)
	if msg != `feat(Pipeline): [IEMS-460] 마감: "close"` {
		t.Fatal(msg)
	}
	body, err := completeFrontmatter("---\nstatus: ready\n---\n\n# x\n", "2026-10-20", finalizeIn{
		CommitType: "feat", CommitScope: "Pipeline", CommitSummary: `마감: "close"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseCommitMeta(body)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Type != "feat" || meta.Scope != "Pipeline" || meta.Summary != `마감: "close"` {
		t.Fatalf("%#v\n%s", meta, body)
	}
	if strings.Count(body, "commit_type:") != 1 {
		t.Fatal(body)
	}
}

func TestGithubCompareURL(t *testing.T) {
	u, err := githubCompareURL("git@github.com:eric2935/iEMS.git", "main", "feature/IEMS-460")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://github.com/eric2935/iEMS/compare/main...feature/IEMS-460?expand=1" {
		t.Fatal(u)
	}
	if _, err := githubCompareURL("/tmp/origin.git", "main", "feature/IEMS-460"); err == nil {
		t.Fatal("expected non-github remote")
	}
}

func TestMoveTodoDoneAppends(t *testing.T) {
	in := "# board\n\n## In Progress\n- [ ] IEMS-460: work\n\n## Backlog\n- [ ] IEMS-402: x\n\n## Done\n> note\n- [x] IEMS-431: old\n"
	out, err := moveTodoDone(in, "IEMS-460", defaultPattern())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "## In Progress\n- [ ] IEMS-460") {
		t.Fatal(out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "- [x] IEMS-460: work") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "- [x] IEMS-431: old\n- [x] IEMS-460: work") {
		t.Fatal(out)
	}
	again, err := moveTodoDone(out, "IEMS-460", defaultPattern())
	if err != nil || again != out {
		t.Fatal(err, again)
	}
}

func TestCommitCandidatesIncludeToolSource(t *testing.T) {
	dir := initRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "03_tools/mcp-hub/cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "03_tools/mcp-hub/bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "03_tools/mcp-hub/cmd/main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "03_tools/mcp-hub/bin/mcp-hub"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := New(dir).commitCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"03_tools/mcp-hub/cmd/main.go", "README.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%#v", got)
	}
}

func TestCloseTicketFastPath(t *testing.T) {
	dir := closeFixture(t, "IEMS-460")
	waited := false
	var out bytes.Buffer
	err := New(dir).CloseTicket(context.Background(), CloseIO{
		Out: &out,
		Err: &out,
		WaitMerge: func(string, string) error {
			waited = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err, out.String())
	}
	if waited || !strings.Contains(out.String(), "FAST_PATH") {
		t.Fatal(out.String())
	}
	if branch := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); branch != "main" {
		t.Fatal(branch)
	}
	if strings.TrimSpace(gitOut(t, dir, "branch", "--list", "feature/IEMS-460")) != "" {
		t.Fatal("feature branch remains")
	}
	body, err := os.ReadFile(filepath.Join(dir, "TODO.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- [x] IEMS-431: old\n- [x] IEMS-460: work") {
		t.Fatal(string(body))
	}
	if strings.TrimSpace(gitOut(t, dir, "status", "--porcelain=v1")) != "" {
		t.Fatal(gitOut(t, dir, "status", "--porcelain=v1"))
	}
}

func TestCloseTicketSurgicalWaitsForMerge(t *testing.T) {
	dir := closeFixture(t, "IEMS-460")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "03_tools/mcp-hub/internal/note.go")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("package mcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "03_tools/mcp-hub/bin/mcp-hub")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := New(dir).CloseTicket(context.Background(), CloseIO{
		Out: &out,
		Err: &out,
		WaitMerge: func(string, string) error {
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "origin/main does not contain") {
		t.Fatal(err, out.String())
	}
	if branch := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); branch != "feature/IEMS-460" {
		t.Fatal(branch)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "TODO.md"))
	if strings.Contains(string(body), "- [x] IEMS-460") {
		t.Fatal(string(body))
	}
	names := gitOut(t, dir, "ls-files")
	if !strings.Contains(names, "03_tools/mcp-hub/internal/note.go") || strings.Contains(names, "03_tools/mcp-hub/bin/mcp-hub") {
		t.Fatal(names)
	}
}

func TestCloseTicketSurgicalThenMain(t *testing.T) {
	dir := closeFixture(t, "IEMS-460")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := New(dir).CloseTicket(context.Background(), CloseIO{
		Out: &out,
		Err: &out,
		WaitMerge: func(string, string) error {
			gitRun(t, dir, "push", "origin", "HEAD:main")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), "feat(Pipeline): [IEMS-460] close") {
		t.Fatal(out.String())
	}
	log := gitOut(t, dir, "log", "--oneline", "origin/main")
	if !strings.Contains(log, "docs(todo): mark IEMS-460 as done") || !strings.Contains(log, "feat(Pipeline): [IEMS-460] close") {
		t.Fatal(log)
	}
	if strings.TrimSpace(gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD")) != "main" {
		t.Fatal("not on main")
	}
}

func closeFixture(t *testing.T, ticket string) string {
	t.Helper()
	origin := t.TempDir()
	gitRun(t, origin, "init", "-b", "main", "--bare")
	dir := initRepo(t)
	todo := "## In Progress\n- [ ] " + ticket + ": work\n\n## Done\n- [x] IEMS-431: old\n"
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte(todo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("obsidian-docs/\n03_tools/mcp-hub/bin/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "TODO.md", ".gitignore")
	gitRun(t, dir, "commit", "-m", "todo")
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "-u", "origin", "main")
	gitRun(t, dir, "checkout", "-b", "feature/"+ticket)
	specDir := filepath.Join(dir, filepath.FromSlash(defaultSpecRel()), "_archive")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "---\nstatus: completed\ncommit_type: \"feat\"\ncommit_scope: \"Pipeline\"\ncommit_summary: \"close\"\nupdated_at: 2026-10-20\n---\n\n# x\n"
	name := "SPEC - [" + ticket + "] [Pipeline] demo.md"
	if err := os.WriteFile(filepath.Join(specDir, name), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
