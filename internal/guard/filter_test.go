package guard

import (
	"testing"

	"iems/mcp-hub/internal/config"
)

func TestTicketAndSlugBoundaries(t *testing.T) {
	pattern := config.DefaultConfig().Pattern()
	okTickets := []string{"IEMS-451", "IEMS-401-2", "IEMS-203-10"}
	badTickets := []string{"IEMS-451-go", "iems-451", "IEMS-4512-go", "IEMS-401-2-layout", "../IEMS-451", `IEMS-451\x`}
	for _, id := range okTickets {
		if err := ValidateTicketID(id, pattern); err != nil {
			t.Fatalf("ticket %s: %v", id, err)
		}
	}
	for _, id := range badTickets {
		if err := ValidateTicketID(id, pattern); err == nil {
			t.Fatalf("ticket %s was accepted", id)
		}
	}

	okSlugs := []string{"", "go", "go-mcp-hub", "a1"}
	badSlugs := []string{"Go", "2layout", "go-", "go--hub", "-go", "go/mcp"}
	for _, slug := range okSlugs {
		if err := ValidateSlug(slug); err != nil {
			t.Fatalf("slug %q: %v", slug, err)
		}
	}
	for _, slug := range badSlugs {
		if err := ValidateSlug(slug); err == nil {
			t.Fatalf("slug %q was accepted", slug)
		}
	}
}

func TestBranchName(t *testing.T) {
	pattern := config.DefaultConfig().Pattern()
	name, err := BranchName("IEMS-451", "", pattern)
	if err != nil || name != "feature/IEMS-451" {
		t.Fatalf("got %s %v", name, err)
	}
	name, err = BranchName("IEMS-451", "go-mcp-hub", pattern)
	if err != nil || name != "feature/IEMS-451-go-mcp-hub" {
		t.Fatalf("got %s %v", name, err)
	}
	name, err = BranchName("IEMS-401-2", "", pattern)
	if err != nil || name != "feature/IEMS-401-2" {
		t.Fatalf("got %s %v", name, err)
	}
}

func TestFinalizeBranchBinding(t *testing.T) {
	cases := []struct {
		branch string
		ticket string
		ok     bool
	}{
		{"feature/IEMS-451", "IEMS-451", true},
		{"feature/IEMS-451-go-mcp-hub", "IEMS-451", true},
		{"feature/IEMS-401-2", "IEMS-401-2", true},
		{"feature/IEMS-401-2-layout", "IEMS-401-2", true},
		{"feature/IEMS-401-2", "IEMS-401", false},
		{"feature/IEMS-401-2-layout", "IEMS-401", false},
		{"feature/IEMS-4512-go", "IEMS-451", false},
		{"feature/IEMS-451-go-", "IEMS-451", false},
		{"feature/IEMS-451-Go", "IEMS-451", false},
		{"main", "IEMS-451", false},
	}
	pattern := config.DefaultConfig().Pattern()
	for _, tc := range cases {
		got, err := FinalizeBranchOK(tc.branch, tc.ticket, pattern)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.branch, tc.ticket, err)
		}
		if got != tc.ok {
			t.Fatalf("%s ticket %s: got %v want %v", tc.branch, tc.ticket, got, tc.ok)
		}
	}
}

func TestBlacklist(t *testing.T) {
	list := config.DefaultConfig().DirtyBlacklist
	blocked := []string{"TODO.md", "obsidian-docs/v2/a.md", "03_tools/mcp-hub/go.mod", ".cursor/mcp.json", "repomix-output.xml"}
	allowed := []string{".cursorrules", "02_labs/go-lab/go.mod", "README.md"}
	for _, p := range blocked {
		if !IsBlacklisted(p, list) {
			t.Fatalf("expected blacklist %s", p)
		}
	}
	for _, p := range allowed {
		if IsBlacklisted(p, list) {
			t.Fatalf("expected stageable %s", p)
		}
	}
	stageable, excluded := SplitDirty([]string{"TODO.md", "README.md", "03_tools/a.go"}, list)
	if len(stageable) != 1 || stageable[0] != "README.md" {
		t.Fatalf("stageable %#v", stageable)
	}
	if len(excluded) != 2 {
		t.Fatalf("excluded %#v", excluded)
	}
}

func TestCommitExcludedKeepsToolSource(t *testing.T) {
	closeList := config.DefaultConfig().CloseExcludePaths
	dirty := config.DefaultConfig().DirtyBlacklist
	if IsCommitExcluded("03_tools/mcp-hub/internal/pipeline/tools.go", closeList) || IsCommitExcluded("03_tools/mcp-hub/go.mod", closeList) {
		t.Fatal("source must be committable")
	}
	blocked := []string{"TODO.md", "obsidian-docs/v2/a.md", ".cursor/mcp.json", "repomix-output.xml", "03_tools/mcp-hub/bin/mcp-hub"}
	for _, p := range blocked {
		if !IsCommitExcluded(p, closeList) {
			t.Fatalf("expected commit exclude %s", p)
		}
	}
	if !IsBlacklisted("03_tools/mcp-hub/internal/pipeline/tools.go", dirty) {
		t.Fatal("prepare gate still excludes 03_tools")
	}
}

func TestParseFeatureTicket(t *testing.T) {
	cases := []struct {
		branch string
		ticket string
		ok     bool
	}{
		{"feature/IEMS-460", "IEMS-460", true},
		{"feature/IEMS-401-2", "IEMS-401-2", true},
		{"feature/IEMS-451-go-mcp-hub", "IEMS-451", true},
		{"feature/IEMS-460-", "", false},
		{"main", "", false},
		{"feature/IEMS-451-Go", "", false},
	}
	pattern := config.DefaultConfig().Pattern()
	for _, tc := range cases {
		got, err := ParseFeatureTicket(tc.branch, pattern)
		if tc.ok {
			if err != nil || got != tc.ticket {
				t.Fatalf("%s: got %s %v", tc.branch, got, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s: expected error", tc.branch)
		}
	}
}
