package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClipboardCommandLinuxFallback(t *testing.T) {
	name, args, err := clipboardCommand("linux", func(bin string) (string, error) {
		if bin == "wl-paste" {
			return bin, nil
		}
		return "", os.ErrNotExist
	})
	if err != nil || name != "wl-paste" || args != nil {
		t.Fatalf("%s %v %v", name, args, err)
	}
	name, args, err = clipboardCommand("linux", func(bin string) (string, error) {
		if bin == "xclip" {
			return bin, nil
		}
		return "", os.ErrNotExist
	})
	if err != nil || name != "xclip" || len(args) != 3 {
		t.Fatalf("%s %v %v", name, args, err)
	}
	if _, _, err = clipboardCommand("linux", func(string) (string, error) {
		return "", os.ErrNotExist
	}); err == nil || !strings.Contains(err.Error(), "ERR_CLIPBOARD") {
		t.Fatal(err)
	}
}

func TestIngestClipboardTargetTodo(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	h := clipboardHub(t, dir, fencedSpec("Feat", "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest", "marker-one"))
	out, err := h.IngestClipboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !out.TodoUpdated || out.TodoLine != "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest" {
		t.Fatalf("%#v", out)
	}
	if !strings.Contains(out.SpecFilePath, "SPEC - [IEMS-457] [Pipeline] clipboard ingest.md") {
		t.Fatal(out.SpecFilePath)
	}
	body := readRepoFile(t, dir, out.SpecFilePath)
	if !strings.Contains(body, "marker-one") || strings.Contains(body, "```") {
		t.Fatalf("%s", body)
	}
	todo := readRepoFile(t, dir, "TODO.md")
	if !strings.Contains(todo, "## In Progress\n"+out.TodoLine+"\n") {
		t.Fatalf("%s", todo)
	}

	h.ReadClipboard = func(context.Context) (string, error) {
		return fencedSpec("Feat", "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest", "marker-two"), nil
	}
	out, err = h.IngestClipboard(context.Background())
	if err != nil || out.TodoUpdated {
		t.Fatal(err, out.TodoUpdated)
	}
	body = readRepoFile(t, dir, out.SpecFilePath)
	if !strings.Contains(body, "marker-two") {
		t.Fatalf("%s", body)
	}
	if strings.Count(readRepoFile(t, dir, "TODO.md"), "IEMS-457") != 1 {
		t.Fatal(readRepoFile(t, dir, "TODO.md"))
	}
}

func TestIngestClipboardBuildsTodoFromCommitType(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	raw := "``````markdown\n---\ncommit_type: Docs\nstatus: ready\n---\n\n# [Spec] [IEMS-457] [Pipeline] clipboard ingest\n\n본문에 SPEC - [IEMS-457] [Pipeline] clipboard ingest.md 가 있어도 된다.\n``````"
	h := clipboardHub(t, dir, raw)
	out, err := h.IngestClipboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "- [ ] IEMS-457: [Docs](Pipeline): clipboard ingest"
	if out.TodoLine != want || !out.TodoUpdated {
		t.Fatalf("%#v", out)
	}
}

func TestIngestClipboardAllowsTodoMdInTitle(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	const title = "클립보드 기반 Spec 파일 및 TODO.md 원클릭 자동 인제스트 구축"
	raw := "---\ncommit_type: Feat\nstatus: ready\n---\n\n# [Spec] [IEMS-457] [Pipeline] " + title + "\n\n> Target Todo: - [ ] IEMS-457: [Feat](Pipeline): " + title + "\n"
	h := clipboardHub(t, dir, raw)
	out, err := h.IngestClipboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.TodoLine != "- [ ] IEMS-457: [Feat](Pipeline): "+title || !out.TodoUpdated {
		t.Fatalf("%#v", out)
	}
	if !strings.Contains(out.SpecFilePath, title+".md") {
		t.Fatal(out.SpecFilePath)
	}
	if !strings.Contains(readRepoFile(t, dir, "TODO.md"), title) {
		t.Fatal(readRepoFile(t, dir, "TODO.md"))
	}
}

func TestIngestClipboardDefaultsFeat(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	raw := "---\nstatus: ready\n---\n\n# [Spec] [IEMS-457] [Pipeline] clipboard ingest\n"
	h := clipboardHub(t, dir, raw)
	out, err := h.IngestClipboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.TodoLine != "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest" {
		t.Fatal(out.TodoLine)
	}
}

func TestIngestClipboardRejectsBeforeWrite(t *testing.T) {
	t.Parallel()
	cases := []string{
		"hello",
		"---\nstatus: ready\n---\n\n# not a spec\n",
		fencedSpec("Feat", "- [ ] IEMS-458: [Feat](Pipeline): clipboard ingest", "x"),
		fencedSpec("Feat", "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest (Spec: a)", "x"),
		fencedSpec("Feat", "- [ ] IEMS-457: [Feat](Pipeline): see SPEC - [IEMS-457] clipboard ingest", "x"),
	}
	for _, raw := range cases {
		dir := clipboardWorkspace(t)
		h := clipboardHub(t, dir, raw)
		_, err := h.IngestClipboard(context.Background())
		if err == nil || !strings.Contains(err.Error(), "ERR_SPEC_MALFORMED") {
			t.Fatal(err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "obsidian-docs")); !os.IsNotExist(statErr) {
			t.Fatal("spec dir was created", statErr)
		}
		todo := readRepoFile(t, dir, "TODO.md")
		if strings.Contains(todo, "IEMS-457") || strings.Contains(todo, "IEMS-458") {
			t.Fatal(todo)
		}
	}
}

func TestIngestClipboardEmptyOrUnreadable(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	h := clipboardHub(t, dir, "   \n")
	if _, err := h.IngestClipboard(context.Background()); err == nil || !strings.Contains(err.Error(), "ERR_CLIPBOARD") {
		t.Fatal(err)
	}
	h.ReadClipboard = func(context.Context) (string, error) {
		return "", os.ErrNotExist
	}
	if _, err := h.IngestClipboard(context.Background()); err == nil || !strings.Contains(err.Error(), "ERR_CLIPBOARD") {
		t.Fatal(err)
	}
	h.ReadClipboard = nil
	if _, err := h.IngestClipboard(context.Background()); err == nil || !strings.Contains(err.Error(), "ERR_CLIPBOARD") {
		t.Fatal(err)
	}
}

func TestInvokeClipboardOnlyWhenArgsEmpty(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	h := clipboardHub(t, dir, fencedSpec("Feat", "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest", "from-clipboard"))
	outRaw, err := h.Invoke("iems_ingest_spec", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	out := outRaw.(ingestOut)
	if !strings.Contains(readRepoFile(t, dir, out.SpecFilePath), "from-clipboard") {
		t.Fatal(out)
	}

	called := false
	h.ReadClipboard = func(context.Context) (string, error) {
		called = true
		return "", os.ErrNotExist
	}
	_, err = h.Invoke("iems_ingest_spec", []byte(`{"ticket_id":"IEMS-457","scope":"Pipeline","title":"clipboard ingest","spec_markdown":"---\nstatus: ready\n---\n# two\n","todo_line":"- [ ] IEMS-457: again"}`))
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("explicit args read the clipboard")
	}
	if _, err = h.Invoke("iems_ingest_spec", []byte(`{"ticket_id":"IEMS-457"}`)); err == nil || !strings.Contains(err.Error(), "ERR_SPEC_MALFORMED") {
		t.Fatal(err)
	}
	if called {
		t.Fatal("partial args read the clipboard")
	}
}

func TestIngestClipboardJSONFields(t *testing.T) {
	t.Parallel()
	dir := clipboardWorkspace(t)
	h := clipboardHub(t, dir, fencedSpec("Feat", "- [ ] IEMS-457: [Feat](Pipeline): clipboard ingest", "marker"))
	out, err := h.IngestClipboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"spec_file_path", "todo_line", "todo_updated"} {
		if _, ok := got[key]; !ok {
			t.Fatal(string(body))
		}
	}
}

func clipboardWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePipelineConfig(t, dir, "IEMS", "obsidian-docs/v2")
	if err := os.WriteFile(filepath.Join(dir, "TODO.md"), []byte("## In Progress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func clipboardHub(t *testing.T, dir, raw string) *Hub {
	t.Helper()
	h := New(dir)
	h.ReadClipboard = func(context.Context) (string, error) {
		return raw, nil
	}
	return h
}

func fencedSpec(commitType, todo, marker string) string {
	return "```markdown\n---\ncommit_type: " + commitType + "\nstatus: ready\n---\n\n# [Spec] [IEMS-457] [Pipeline] clipboard ingest\n\n> Target Todo: " + todo + "\n\n" + marker + "\n```\n"
}

func readRepoFile(t *testing.T, dir, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
