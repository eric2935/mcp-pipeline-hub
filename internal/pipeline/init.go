package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"iems/mcp-hub/internal/config"
	"iems/mcp-hub/internal/scaffold"
)

var gitignoreEnsure = []string{
	"obsidian-docs/",
	"repomix-output.xml",
	"bin/",
	".cursor/",
}

// InitOptions controls -init.
// Empty Prefix becomes PROJ. Empty ProjectName becomes the directory base name.
// Empty CreatedAt becomes the local date. Force overwrites scaffold files except .gitignore.
type InitOptions struct {
	Prefix      string
	Vault       string
	ProjectName string
	Force       bool
	CreatedAt   string
}

// InitReport lists files created, appended, left untouched, or replaced.
type InitReport struct {
	Created     []string `json:"created"`
	Updated     []string `json:"updated"`
	Skipped     []string `json:"skipped"`
	Overwritten []string `json:"overwritten,omitempty"`
}

// RunInitProject writes the pipeline skeleton and governance templates.
// Existing scaffold files are left in place unless Force is set.
// .gitignore is never replaced; missing lines are appended.
func RunInitProject(dir string, opt InitOptions) (InitReport, error) {
	var report InitReport
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return report, err
	}
	prefix := strings.TrimSpace(opt.Prefix)
	if prefix == "" {
		prefix = "PROJ"
	}
	project := strings.TrimSpace(opt.ProjectName)
	if project == "" {
		project = filepath.Base(absDir)
	}
	files, err := scaffold.Render(scaffold.Context{
		ProjectName:  project,
		TicketPrefix: prefix,
		CreatedAt:    opt.CreatedAt,
		TargetRoot:   absDir,
		VaultDir:     opt.Vault,
	})
	if err != nil {
		var cfgErr *config.Error
		if errors.As(err, &cfgErr) {
			return report, &ToolError{Code: "ERR_CONFIG_INVALID", Detail: err.Error()}
		}
		return report, err
	}
	written, err := scaffold.Write(absDir, files, opt.Force)
	report.Created = append(report.Created, written.Created...)
	report.Skipped = append(report.Skipped, written.Skipped...)
	report.Overwritten = append(report.Overwritten, written.Overwritten...)
	if err != nil {
		return report, err
	}
	if err := ensureGitignore(absDir, &report); err != nil {
		return report, err
	}
	cfg, err := config.InitTemplate(prefix, opt.Vault)
	if err != nil {
		return report, &ToolError{Code: "ERR_CONFIG_INVALID", Detail: err.Error()}
	}
	dirs := []string{
		filepath.Join(cfg.VaultDir, cfg.SpecDir),
		filepath.Join(cfg.VaultDir, cfg.SpecDir, cfg.SpecArchiveDir),
	}
	for _, rel := range dirs {
		full := filepath.Join(absDir, filepath.FromSlash(rel))
		if _, statErr := os.Stat(full); statErr == nil {
			report.Skipped = append(report.Skipped, filepath.ToSlash(rel))
			continue
		}
		if err := os.MkdirAll(full, 0o755); err != nil {
			return report, err
		}
		report.Created = append(report.Created, filepath.ToSlash(rel))
	}
	return report, nil
}

func ensureGitignore(root string, report *InitReport) error {
	rel := ".gitignore"
	full := filepath.Join(root, rel)
	body, err := os.ReadFile(full)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		text := strings.Join(gitignoreEnsure, "\n") + "\n"
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			return err
		}
		report.Created = append(report.Created, rel)
		return nil
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, line := range gitignoreEnsure {
		if !have[line] {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		report.Skipped = append(report.Skipped, rel)
		return nil
	}
	text := string(body)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += strings.Join(missing, "\n") + "\n"
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return err
	}
	report.Updated = append(report.Updated, rel)
	return nil
}
