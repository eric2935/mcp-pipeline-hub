package config

import (
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ErrInvalid  = "ERR_CONFIG_INVALID"
	ErrNotFound = "ERR_CONFIG_NOT_FOUND"

	initToolName = "agentic-mcp-hub"
)

var prefixPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// Error is a config load or validation failure.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

// PipelineConfig is the .mcp-pipeline.json document.
type PipelineConfig struct {
	ToolName          string   `json:"tool_name"`
	TicketPrefix      string   `json:"ticket_prefix"`
	TicketPattern     string   `json:"ticket_pattern"`
	VaultDir          string   `json:"vault_dir"`
	SpecDir           string   `json:"spec_dir"`
	SpecArchiveDir    string   `json:"spec_archive_dir"`
	TodoFile          string   `json:"todo_file"`
	SSOTManifests     []string `json:"ssot_manifests"`
	DirtyBlacklist    []string `json:"dirty_blacklist"`
	CloseExcludePaths []string `json:"close_exclude_paths"`

	compiledPattern *regexp.Regexp
	specHeader      *regexp.Regexp
	todoTicket      *regexp.Regexp
}

// DefaultConfig is the iEMS fallback used when .mcp-pipeline.json is absent.
func DefaultConfig() *PipelineConfig {
	c := &PipelineConfig{
		ToolName:       "iems-mcp-hub",
		TicketPrefix:   "IEMS",
		TicketPattern:  `^IEMS-[0-9]+(?:-[0-9]+)?$`,
		VaultDir:       "obsidian-docs/v2",
		SpecDir:        "03_Spec (구현명세서)",
		SpecArchiveDir: "_archive",
		TodoFile:       "TODO.md",
		SSOTManifests: []string{
			"02_labs/go-lab/go.mod",
			"02_labs/ng-lab/iems-ui/package.json",
		},
		DirtyBlacklist: []string{
			"TODO.md",
			"repomix-output.xml",
			"obsidian-docs/",
			"03_tools/",
			".cursor/",
		},
		CloseExcludePaths: []string{
			"obsidian-docs/",
			"TODO.md",
			".cursor/",
			"repomix-output.xml",
			"03_tools/mcp-hub/bin/",
		},
	}
	if err := c.Validate(); err != nil {
		panic(err)
	}
	return c
}

// InitTemplate is the bleached document -init writes for a new project.
// Ticket prefix case is preserved. SSOT and close paths do not copy the iEMS defaults.
func InitTemplate(prefix, vault string) (*PipelineConfig, error) {
	if !prefixPattern.MatchString(prefix) {
		return nil, &Error{Code: ErrInvalid, Detail: "ticket_prefix"}
	}
	if vault == "" {
		vault = "obsidian-docs/v2"
	}
	base := &PipelineConfig{
		ToolName:       initToolName,
		TicketPrefix:   prefix,
		TicketPattern:  "^" + regexp.QuoteMeta(prefix) + `-[0-9]+(?:-[0-9]+)?$`,
		VaultDir:       vault,
		SpecDir:        "03_Spec (구현명세서)",
		SpecArchiveDir: "_archive",
		TodoFile:       "TODO.md",
		SSOTManifests:  []string{},
		DirtyBlacklist: []string{
			"TODO.md",
			"repomix-output.xml",
			"obsidian-docs/",
			".cursor/",
		},
		CloseExcludePaths: []string{
			"obsidian-docs/",
			"TODO.md",
			".cursor/",
			"repomix-output.xml",
		},
	}
	if err := base.Validate(); err != nil {
		return nil, err
	}
	return base, nil
}

// Validate checks required fields and compiles ticket_pattern as a full match.
func (c *PipelineConfig) Validate() error {
	if c == nil {
		return &Error{Code: ErrInvalid, Detail: "nil config"}
	}
	required := []struct{ name, val string }{
		{"tool_name", c.ToolName},
		{"ticket_prefix", c.TicketPrefix},
		{"ticket_pattern", c.TicketPattern},
		{"vault_dir", c.VaultDir},
		{"spec_dir", c.SpecDir},
		{"spec_archive_dir", c.SpecArchiveDir},
		{"todo_file", c.TodoFile},
	}
	for _, field := range required {
		if strings.TrimSpace(field.val) == "" {
			return &Error{Code: ErrInvalid, Detail: field.name}
		}
	}
	if !strings.HasPrefix(c.TicketPattern, "^") || !strings.HasSuffix(c.TicketPattern, "$") {
		return &Error{Code: ErrInvalid, Detail: "ticket_pattern"}
	}
	if strings.Contains(strings.ReplaceAll(c.TicketPattern, "(?:", ""), "(") {
		return &Error{Code: ErrInvalid, Detail: "ticket_pattern"}
	}
	re, err := regexp.Compile(c.TicketPattern)
	if err != nil {
		return &Error{Code: ErrInvalid, Detail: "ticket_pattern"}
	}
	inner := strings.TrimPrefix(c.TicketPattern, "^")
	inner = strings.TrimSuffix(inner, "$")
	header, err := regexp.Compile(`^# \[Spec\] \[(` + inner + `)\] \[([^\]\r\n]+)\] (.+)$`)
	if err != nil {
		return &Error{Code: ErrInvalid, Detail: "ticket_pattern"}
	}
	todo, err := regexp.Compile(`^- \[ \] (` + inner + `): `)
	if err != nil {
		return &Error{Code: ErrInvalid, Detail: "ticket_pattern"}
	}
	c.compiledPattern = re
	c.specHeader = header
	c.todoTicket = todo
	return nil
}

// GetSpecActiveDir is the active Spec directory relative to the workspace.
func (c *PipelineConfig) GetSpecActiveDir() string {
	return filepath.ToSlash(filepath.Join(c.VaultDir, c.SpecDir))
}

// GetSpecArchiveDir is the Spec archive directory relative to the workspace.
func (c *PipelineConfig) GetSpecArchiveDir() string {
	return filepath.ToSlash(filepath.Join(c.VaultDir, c.SpecDir, c.SpecArchiveDir))
}

// Pattern is the compiled full-match ticket expression.
func (c *PipelineConfig) Pattern() *regexp.Regexp {
	if c == nil {
		return nil
	}
	return c.compiledPattern
}

// SpecHeaderPattern matches a Spec H1 and captures ticket, scope, and title.
func (c *PipelineConfig) SpecHeaderPattern() *regexp.Regexp {
	if c == nil {
		return nil
	}
	return c.specHeader
}

// TodoTicketPattern matches a TODO checkbox line and captures the ticket.
func (c *PipelineConfig) TodoTicketPattern() *regexp.Regexp {
	if c == nil {
		return nil
	}
	return c.todoTicket
}

// MatchTicket reports whether ticket matches the full ticket pattern.
func (c *PipelineConfig) MatchTicket(ticket string) bool {
	re := c.Pattern()
	if re == nil {
		return false
	}
	loc := re.FindStringIndex(ticket)
	return loc != nil && loc[0] == 0 && loc[1] == len(ticket)
}
