package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"mcp-pipeline-hub/internal/config"
	"mcp-pipeline-hub/internal/pipeline"
	"mcp-pipeline-hub/internal/protocol"
)

var copyToClipboard = pipeline.WriteClipboard

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp-hub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "workspace root")
	configPath := fs.String("config", "", "pipeline config path; default is <root>/.mcp-pipeline.json")
	ingest := fs.Bool("ingest-clipboard", false, "ingest a Spec from the OS clipboard")
	closeTicket := fs.Bool("close-ticket", false, "commit, push, and finish the current feature branch after the human merges")
	issueToken := fs.String("issue-token", "", "issue a one-time finalize token for a ticket id")
	tokenAlias := fs.String("token", "", "alias of -issue-token")
	initProject := fs.Bool("init", false, "create pipeline files in the workspace")
	prefix := fs.String("prefix", "", "ticket prefix for -init; default PROJ")
	vault := fs.String("vault", "", "vault directory for -init")
	project := fs.String("project", "", "project name for -init; default is the directory name")
	force := fs.Bool("force", false, "overwrite existing scaffold files")
	debugFlag := fs.Bool("debug", false, "write error type, unwrap chain, and context to stderr")
	version := fs.Bool("version", false, "print mcp-hub version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	debug := *debugFlag || envDebug()
	issueA := strings.TrimSpace(*issueToken)
	issueB := strings.TrimSpace(*tokenAlias)
	if issueA != "" && issueB != "" && issueA != issueB {
		fmt.Fprintln(stderr, "mcp-hub: -issue-token and -token values differ")
		return 2
	}
	issueID := issueA
	if issueID == "" {
		issueID = issueB
	}
	modes := 0
	if *ingest {
		modes++
	}
	if *closeTicket {
		modes++
	}
	if *initProject {
		modes++
	}
	if issueID != "" {
		modes++
	}
	if *version {
		modes++
	}
	if modes > 1 {
		fmt.Fprintln(stderr, "mcp-hub: -init, -ingest-clipboard, -close-ticket, -issue-token, and -version are mutually exclusive")
		return 2
	}
	if *version {
		fmt.Fprintf(stdout, "mcp-hub %s\n", protocol.ServerVersion)
		return 0
	}
	if (*prefix != "" || *vault != "" || *project != "" || *force) && !*initProject {
		fmt.Fprintln(stderr, "mcp-hub: -prefix, -vault, -project, and -force require -init")
		return 2
	}
	workspace := *root
	if workspace == "" {
		workspace = "."
	}
	if *initProject {
		report, err := pipeline.RunInitProject(workspace, pipeline.InitOptions{
			Prefix:      *prefix,
			Vault:       *vault,
			ProjectName: *project,
			Force:       *force,
		})
		if err != nil {
			writeCLIError(stderr, err, debug)
			return 1
		}
		writeInitLog(stderr, report)
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	hub, err := pipeline.Open(workspace, *configPath)
	if err != nil {
		writeCLIError(stderr, err, debug)
		return 1
	}
	if *closeTicket {
		if err := hub.CloseTicket(context.Background(), pipeline.CloseIO{In: stdin, Out: stdout, Err: stderr}); err != nil {
			writeCLIError(stderr, err, debug)
			return 1
		}
		return 0
	}
	if *ingest {
		out, err := hub.IngestClipboard(context.Background())
		if err != nil {
			writeCLIError(stderr, err, debug)
			return 1
		}
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if issueID != "" {
		token, err := hub.IssueFinalizeToken(issueID)
		if err != nil {
			writeCLIError(stderr, err, debug)
			return 1
		}
		fmt.Fprintln(stdout, token)
		if err := copyToClipboard(context.Background(), token); err != nil {
			fmt.Fprintln(stderr, "WARN: clipboard copy failed")
		}
		return 0
	}
	if *root == "" {
		fmt.Fprintln(stderr, "mcp-hub: -root is required")
		return 2
	}
	if err := protocol.Serve(stdin, stdout, hub); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func writeInitLog(stderr io.Writer, report pipeline.InitReport) {
	for _, path := range report.Created {
		fmt.Fprintf(stderr, "[CREATE] %s\n", path)
	}
	for _, path := range report.Skipped {
		fmt.Fprintf(stderr, "[SKIP] %s\n", path)
	}
	for _, path := range report.Overwritten {
		fmt.Fprintf(stderr, "[OVERWRITE] %s\n", path)
	}
	for _, path := range report.Updated {
		fmt.Fprintf(stderr, "[UPDATE] %s\n", path)
	}
}

func envDebug() bool {
	v := strings.TrimSpace(os.Getenv("MCP_DEBUG"))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true")
}

func writeCLIError(stderr io.Writer, err error, debug bool) {
	if err == nil {
		return
	}
	line, code, detail, domain := cliErrorLine(err)
	fmt.Fprintln(stderr, line)
	if !debug {
		return
	}
	fmt.Fprintf(stderr, "[DEBUG] error type: %T\n", err)
	writeUnwrap(stderr, err)
	if domain {
		fmt.Fprintf(stderr, "[DEBUG] context: code=%s detail=%s\n", code, detail)
		return
	}
	fmt.Fprintf(stderr, "[DEBUG] context: message=%s\n", oneLine(err.Error()))
}

func cliErrorLine(err error) (line, code, detail string, domain bool) {
	var toolErr *pipeline.ToolError
	if errors.As(err, &toolErr) {
		code = toolErr.Code
		detail = sanitizeDetail(toolErr.Detail)
		return formatCodeDetail(code, detail), code, detail, true
	}
	var cfgErr *config.Error
	if errors.As(err, &cfgErr) {
		code = cfgErr.Code
		detail = sanitizeDetail(cfgErr.Detail)
		return formatCodeDetail(code, detail), code, detail, true
	}
	return oneLine(err.Error()), "", "", false
}

func formatCodeDetail(code, detail string) string {
	if detail == "" {
		return code
	}
	return code + ": " + detail
}

func sanitizeDetail(detail string) string {
	return oneLine(strings.TrimSpace(detail))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func writeUnwrap(stderr io.Writer, err error) {
	next := errors.Unwrap(err)
	if next == nil {
		fmt.Fprintln(stderr, "[DEBUG] unwrap: <none>")
		return
	}
	for seen := 0; next != nil && seen < 16; seen++ {
		fmt.Fprintf(stderr, "[DEBUG] unwrap: %s\n", oneLine(next.Error()))
		next = errors.Unwrap(next)
	}
}
