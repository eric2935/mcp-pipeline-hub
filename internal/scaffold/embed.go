package scaffold

import "embed"

// Templates is the scaffold asset set compiled into the mcp-hub binary.
//
//go:embed templates/*
var Templates embed.FS
