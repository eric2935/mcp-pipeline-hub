package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"mcp-pipeline-hub/internal/config"
	"mcp-pipeline-hub/internal/pipeline"
	"mcp-pipeline-hub/internal/protocol"
)

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
	initProject := fs.Bool("init", false, "create pipeline files in the workspace")
	prefix := fs.String("prefix", "", "ticket prefix for -init; default PROJ")
	vault := fs.String("vault", "", "vault directory for -init")
	project := fs.String("project", "", "project name for -init; default is the directory name")
	force := fs.Bool("force", false, "overwrite existing scaffold files")
	if err := fs.Parse(args); err != nil {
		return 2
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
	if modes > 1 {
		fmt.Fprintln(stderr, "mcp-hub: -init, -ingest-clipboard, and -close-ticket are mutually exclusive")
		return 2
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
			writeCLIError(stderr, err)
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
		writeCLIError(stderr, err)
		return 1
	}
	if *closeTicket {
		if err := hub.CloseTicket(context.Background(), pipeline.CloseIO{In: stdin, Out: stdout, Err: stderr}); err != nil {
			writeCLIError(stderr, err)
			return 1
		}
		return 0
	}
	if *ingest {
		out, err := hub.IngestClipboard(context.Background())
		if err != nil {
			writeCLIError(stderr, err)
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

func writeCLIError(stderr io.Writer, err error) {
	var toolErr *pipeline.ToolError
	if errors.As(err, &toolErr) {
		fmt.Fprintln(stderr, toolErr.Code)
		return
	}
	var cfgErr *config.Error
	if errors.As(err, &cfgErr) {
		fmt.Fprintln(stderr, cfgErr.Code)
		return
	}
	fmt.Fprintln(stderr, err)
}
