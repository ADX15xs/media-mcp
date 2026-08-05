package main

import (
	"fmt"
	"log"
	"media-mcp/internal/config"
	"media-mcp/internal/mcp"
	"media-mcp/internal/supplier"
	"os"
	"path/filepath"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("media-mcp %s\n", version)
		return
	}

	// Determine config path.
	cfgPath := "config.yml"
	if len(os.Args) > 1 && os.Args[1] == "--config" && len(os.Args) > 2 {
		cfgPath = os.Args[2]
	} else if p := os.Getenv("MEDIA_MCP_CONFIG"); p != "" {
		cfgPath = p
	} else {
		exe, err := os.Executable()
		if err == nil {
			candidate := filepath.Join(filepath.Dir(exe), "config.yml")
			if _, err := os.Stat(candidate); err == nil {
				cfgPath = candidate
			}
		}
		if _, err := os.Stat(cfgPath); err != nil {
			cfgPath = filepath.Join(".", "config.yml")
		}
	}

	fmt.Fprintf(os.Stderr, "[media-mcp] Loading config from: %s\n", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// --- Build suppliers from config via the registry ---
	// Adapters self-register via init() in their respective files.
	// Unregistered names fall back to HTTPGenericAdapter or
	// HTTPGenericVideoAdapter automatically.
	imageSuppliers, videoSuppliers, buildErrs := supplier.BuildAll(cfg)

	for _, err := range buildErrs {
		fmt.Fprintf(os.Stderr, "[media-mcp] Warning: %v\n", err)
	}

	if len(imageSuppliers) == 0 && len(videoSuppliers) == 0 {
		log.Fatal("No enabled suppliers could be built. Please check config.yml")
	}

	for _, sup := range imageSuppliers {
		fmt.Fprintf(os.Stderr, "[media-mcp] Registered image supplier: %s\n", sup.Name())
	}
	for _, sup := range videoSuppliers {
		fmt.Fprintf(os.Stderr, "[media-mcp] Registered video supplier: %s\n", sup.Name())
	}

	// Create MCP server and start listening on stdio.
	server := mcp.NewServer(cfg)
	server.RegisterSuppliers(imageSuppliers, videoSuppliers)

	fmt.Fprintf(os.Stderr, "[media-mcp] Starting MCP server with %d image supplier(s) and %d video supplier(s) on stdio\n", len(imageSuppliers), len(videoSuppliers))
	if err := server.Start(); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}
}
