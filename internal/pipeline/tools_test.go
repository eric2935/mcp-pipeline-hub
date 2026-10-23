package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iems/mcp-hub/internal/config"
)

func TestStripSixBacktickFence(t *testing.T) {
	raw := "``````markdown\nintro\n---\nstatus: ready\n---\n# body\n``````"
	got, err := stripToFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "---\nstatus: ready\n") {
		t.Fatalf("%q", got)
	}
	if _, err := stripToFrontmatter("no frontmatter"); err == nil || !strings.Contains(err.Error(), "ERR_SPEC_MALFORMED") {
		t.Fatal(err)
	}
}

func TestTodoInsertIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	todo := filepath.Join(dir, "TODO.md")
	initial := "## Backlog\n\n## In Progress\n\n## Done\n"
	if err := os.WriteFile(todo, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	line := "- [ ] IEMS-451: hub"
	updated, err := insertTodo(todo, "IEMS-451", line)
	if err != nil || !updated {
		t.Fatal(err, updated)
	}
	updated, err = insertTodo(todo, "IEMS-451", line)
	if err != nil || updated {
		t.Fatal("second insert", err, updated)
	}
	body, _ := os.ReadFile(todo)
	if strings.Count(string(body), "IEMS-451") != 1 {
		t.Fatalf("%s", body)
	}
	if !strings.Contains(string(body), "## In Progress\n- [ ] IEMS-451: hub\n") {
		t.Fatalf("%s", body)
	}
	updated, err = insertTodo(todo, "IEMS-401", "- [ ] IEMS-401: parent")
	if err != nil || !updated {
		t.Fatal(err)
	}
	if err := os.WriteFile(todo, []byte("## In Progress\n- [ ] IEMS-401-2: child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, err = insertTodo(todo, "IEMS-401", "- [ ] IEMS-401: parent")
	if err != nil || !updated {
		t.Fatalf("parent ticket was treated as present: %v %v", err, updated)
	}
}

func TestCompleteFrontmatter(t *testing.T) {
	in := "---\nid: spec\nstatus: ready\nupdated_at: 2026-10-01\n---\n\nstatus: ready in body\n"
	got, err := completeFrontmatter(in, "2026-10-18", finalizeIn{CommitType: "feat", CommitScope: "Pipeline", CommitSummary: "hub"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "status: completed\n") || !strings.Contains(got, "updated_at: 2026-10-18\n") || !strings.Contains(got, "commit_summary: \"hub\"\n") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "status: ready in body") {
		t.Fatal("body status was rewritten")
	}
}

func TestIngestRejectsPathEscape(t *testing.T) {
	h := New(t.TempDir())
	_, err := h.IngestSpec(ingestIn{TicketID: "../IEMS-451", Scope: "Tooling", Title: "Hub", SpecMarkdown: "---\nstatus: ready\n---\n"})
	if err == nil || !strings.Contains(err.Error(), "ERR_INVALID_PATH") {
		t.Fatal(err)
	}
}

func TestIngestOverwriteAndTodo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("## In Progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	in := ingestIn{
		TicketID:     "IEMS-451",
		Scope:        "Tooling",
		Title:        "Hub",
		SpecMarkdown: "``````md\n---\nstatus: ready\n---\n# one\n``````",
		TodoLine:     "- [ ] IEMS-451: hub",
	}
	out, err := h.IngestSpec(in)
	if err != nil {
		t.Fatal(err)
	}
	if !out.TodoUpdated || !strings.Contains(out.SpecFilePath, "SPEC - [IEMS-451] [Tooling] Hub.md") {
		t.Fatalf("%#v", out)
	}
	in.SpecMarkdown = "---\nstatus: ready\n---\n# two\n"
	in.TodoLine = "- [ ] IEMS-451: again"
	out, err = h.IngestSpec(in)
	if err != nil || out.TodoUpdated {
		t.Fatal(err, out.TodoUpdated)
	}
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(out.SpecFilePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# two") {
		t.Fatalf("%s", body)
	}
}

func TestPrepareBranchFromHeadAndDirtyGuard(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	no := false
	_, err := h.PrepareBranch(context.Background(), prepareIn{TicketID: "IEMS-451", SyncMain: &no})
	if err == nil || !strings.Contains(err.Error(), "ERR_DIRTY_WORKTREE") {
		t.Fatal(err)
	}
	yes := true
	out, err := h.PrepareBranch(context.Background(), prepareIn{TicketID: "IEMS-451", Slug: "go-mcp-hub", SyncMain: &no, AllowDirty: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if out.BranchName != "feature/IEMS-451-go-mcp-hub" || len(out.BaseCommit) < 7 {
		t.Fatalf("%#v", out)
	}
	if branch := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); branch != out.BranchName {
		t.Fatal(branch)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(body) != "dirty\n" {
		t.Fatalf("dirty file changed: %s", body)
	}
}

func TestPrepareBranchExistingIsNotReset(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature/IEMS-451")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "README.md")
	gitRun(t, dir, "commit", "-m", "feature")
	feature := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	gitRun(t, dir, "checkout", "main")
	h := New(dir)
	no := false
	out, err := h.PrepareBranch(context.Background(), prepareIn{TicketID: "IEMS-451", SyncMain: &no})
	if err != nil {
		t.Fatal(err)
	}
	if out.BaseCommit != feature {
		t.Fatalf("branch was recreated: %s want %s", out.BaseCommit, feature)
	}
}

func TestPrepareBranchSyncMainRestoresTodo(t *testing.T) {
	origin := t.TempDir()
	gitRun(t, origin, "init", "-b", "main", "--bare")
	dir := initRepo(t)
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "-u", "origin", "main")
	todo := "## In Progress\n- [ ] IEMS-451: keep\n"
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte(todo), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	out, err := h.PrepareBranch(context.Background(), prepareIn{TicketID: "IEMS-452"})
	if err != nil {
		t.Fatal(err)
	}
	if out.BranchName != "feature/IEMS-452" {
		t.Fatal(out.BranchName)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "TODO.md"))
	if string(body) != todo {
		t.Fatalf("todo not restored: %s", body)
	}
}

func TestPrepareBranchStashConflict(t *testing.T) {
	origin := t.TempDir()
	gitRun(t, origin, "init", "-b", "main", "--bare")
	dir := initRepo(t)
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "-u", "origin", "main")
	gitRun(t, dir, "checkout", "-b", "feature/IEMS-451")
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "TODO.md")
	gitRun(t, dir, "commit", "-m", "todo on branch")
	gitRun(t, dir, "push", "-u", "origin", "feature/IEMS-451")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	_, err := h.PrepareBranch(context.Background(), prepareIn{TicketID: "IEMS-451"})
	if err == nil || !strings.Contains(err.Error(), "ERR_STASH_FAILED") {
		t.Fatal(err)
	}
}

func TestInspectSplitsBlacklist(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specDir := filepath.Join(dir, filepath.FromSlash(defaultSpecRel()))
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "SPEC - [IEMS-451] [Tooling] Hub.md"), []byte("---\nstatus: ready\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "_index.md"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	out, err := h.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.CurrentBranch != "main" || len(out.ActiveSpecs) != 1 || !strings.Contains(out.ActiveSpecs[0], "IEMS-451") {
		t.Fatalf("%#v", out)
	}
	if len(out.StageableFiles) != 1 || out.StageableFiles[0] != "README.md" {
		t.Fatalf("stageable %#v", out.StageableFiles)
	}
	if len(out.ExcludedDirtyFiles) != 1 || out.ExcludedDirtyFiles[0] != "TODO.md" {
		t.Fatalf("excluded %#v", out.ExcludedDirtyFiles)
	}
}

func TestFinalizeBranchBindingAndArchive(t *testing.T) {
	dir := initRepo(t)
	h := New(dir)
	h.Now = func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local) }
	writeSpec(t, dir, "SPEC - [IEMS-451] [Tooling] Hub.md")
	_, err := h.Finalize(context.Background(), finalizeIn{TicketID: "IEMS-451", CommitType: "feat", CommitScope: "Tooling", CommitSummary: "hub"})
	if err == nil || !strings.Contains(err.Error(), "ERR_INVALID_BRANCH") {
		t.Fatal(err)
	}
	gitRun(t, dir, "checkout", "-b", "feature/IEMS-401-2")
	_, err = h.Finalize(context.Background(), finalizeIn{TicketID: "IEMS-401", CommitType: "feat", CommitScope: "Tooling", CommitSummary: "hub"})
	if err == nil || !strings.Contains(err.Error(), "ERR_INVALID_BRANCH") {
		t.Fatal(err)
	}
	gitRun(t, dir, "checkout", "-b", "feature/IEMS-451")
	out, err := h.Finalize(context.Background(), finalizeIn{TicketID: "IEMS-451", CommitType: "feat", CommitScope: "Tooling", CommitSummary: "hub"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Mode != "FAST_PATH" || strings.Contains(out.TerminalCommands, "git add .") {
		t.Fatalf("%#v", out)
	}
	archived := filepath.Join(dir, filepath.FromSlash(out.ArchivedSpecPath))
	body, err := os.ReadFile(archived)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "status: completed") || !strings.Contains(string(body), "updated_at: 2026-10-18") {
		t.Fatalf("%s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(defaultSpecRel()), "SPEC - [IEMS-451] [Tooling] Hub.md")); !os.IsNotExist(err) {
		t.Fatal("active spec remains", err)
	}
}

func TestFinalizeAmbiguousAndSurgical(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature/IEMS-451-go-mcp-hub")
	writeSpec(t, dir, "SPEC - [IEMS-451] [Tooling] A.md")
	writeSpec(t, dir, "SPEC - [IEMS-451] [Tooling] B.md")
	h := New(dir)
	_, err := h.Finalize(context.Background(), finalizeIn{TicketID: "IEMS-451", CommitType: "feat", CommitScope: "Tooling", CommitSummary: "hub"})
	if err == nil || !strings.Contains(err.Error(), "ERR_SPEC_AMBIGUOUS") {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, filepath.FromSlash(defaultSpecRel()), "SPEC - [IEMS-451] [Tooling] B.md"))
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("todo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := h.Finalize(context.Background(), finalizeIn{TicketID: "IEMS-451", CommitType: "feat", CommitScope: "UI/Collector", CommitSummary: "hub"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Mode != "SURGICAL_PR" || len(out.StageableFiles) != 1 || out.StageableFiles[0] != "README.md" {
		t.Fatalf("%#v", out)
	}
	if out.TerminalCommands != closeTicketCommand || strings.Contains(out.TerminalCommands, "TODO.md") {
		t.Fatalf("%s", out.TerminalCommands)
	}
	archived := filepath.Join(dir, filepath.FromSlash(out.ArchivedSpecPath))
	body, err := os.ReadFile(archived)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseCommitMeta(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Type != "feat" || meta.Scope != "UI/Collector" || meta.Summary != "hub" {
		t.Fatalf("%#v", meta)
	}
}

func TestPackContextSSOT(t *testing.T) {
	dir := t.TempDir()
	h := New(dir)
	h.Run = scriptRunner{fn: func(ctx context.Context, dir, name string, args []string) (string, string, error) {
		if name != "npx" {
			t.Fatalf("cmd %s", name)
		}
		ssot := config.DefaultConfig().SSOTManifests
		body := "<file path=\"" + ssot[0] + "\"></file><file path=\"" + ssot[1] + "\"></file>"
		return "", "", os.WriteFile(filepath.Join(dir, repomixOutput), []byte(body), 0o644)
	}}
	out, err := h.PackContext(context.Background())
	if err != nil || !out.SSOTVerified || out.SizeBytes == 0 {
		t.Fatal(err, out)
	}
	h.Run = scriptRunner{fn: func(ctx context.Context, dir, name string, args []string) (string, string, error) {
		return "", "", os.WriteFile(filepath.Join(dir, repomixOutput), []byte("missing"), 0o644)
	}}
	_, err = h.PackContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ERR_SSOT_MISSING") {
		t.Fatal(err)
	}
	h.Run = scriptRunner{fn: func(ctx context.Context, dir, name string, args []string) (string, string, error) {
		return "", "", context.DeadlineExceeded
	}}
	_, err = h.PackContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ERR_TIMEOUT") {
		t.Fatal(err)
	}
}

func TestConfigBindsVaultAndTicket(t *testing.T) {
	dir := t.TempDir()
	raw := `{
	  "tool_name": "shop-hub",
	  "ticket_prefix": "SHOP",
	  "ticket_pattern": "^SHOP-[0-9]+(?:-[0-9]+)?$",
	  "vault_dir": "shop-docs",
	  "spec_dir": "specs",
	  "spec_archive_dir": "old",
	  "todo_file": "BOARD.md",
	  "ssot_manifests": ["go.mod"],
	  "dirty_blacklist": ["BOARD.md", "notes/"],
	  "close_exclude_paths": ["BOARD.md"]
	}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp-pipeline.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BOARD.md"), []byte("## In Progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(dir)
	if h.ServerName() != "shop-hub" {
		t.Fatal(h.ServerName())
	}
	out, err := h.IngestSpec(ingestIn{
		TicketID:     "SHOP-12",
		Scope:        "Pipeline",
		Title:        "Bind",
		SpecMarkdown: "---\nstatus: ready\n---\n",
		TodoLine:     "- [ ] SHOP-12: bind",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SpecFilePath != "shop-docs/specs/SPEC - [SHOP-12] [Pipeline] Bind.md" || !out.TodoUpdated {
		t.Fatalf("%#v", out)
	}
	if _, err := h.IngestSpec(ingestIn{TicketID: "IEMS-12", Scope: "Pipeline", Title: "No", SpecMarkdown: "---\nstatus: ready\n---\n"}); err == nil {
		t.Fatal("IEMS ticket must fail under SHOP pattern")
	}
}

func TestDefinitionsIncludeGateText(t *testing.T) {
	defs := New(t.TempDir()).Definitions()
	if len(defs) != 5 {
		t.Fatal(len(defs))
	}
	by := map[string]string{}
	for _, d := range defs {
		by[d.Name] = d.Description
	}
	if !strings.Contains(by["iems_prepare_branch"], "MUST ONLY be called when preparing Phase 2 branch isolation.") {
		t.Fatal(by["iems_prepare_branch"])
	}
	if !strings.Contains(by["iems_finalize_spec"], "마감 승인") {
		t.Fatal(by["iems_finalize_spec"])
	}
}

type scriptRunner struct {
	fn func(ctx context.Context, dir, name string, args []string) (string, string, error)
}

func (s scriptRunner) Run(ctx context.Context, dir, name string, args ...string) (string, string, error) {
	return s.fn(ctx, dir, name, args)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("## In Progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "README.md", "TODO.md")
	gitRun(t, dir, "commit", "-m", "init")
	return dir
}

func writeSpec(t *testing.T, dir, name string) {
	t.Helper()
	specDir := filepath.Join(dir, filepath.FromSlash(defaultSpecRel()))
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nid: spec\nstatus: ready\nupdated_at: 2026-10-01\n---\n\n# body\n"
	if err := os.WriteFile(filepath.Join(specDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
