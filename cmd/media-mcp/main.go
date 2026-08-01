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

func main() {
	// Determine config path.
	cfgPath := "config.yml"
	if len(os.Args) > 1 && os.Args[1] == "--config" && len(os.Args) > 2 {
		cfgPath = os.Args[2]
	} else if p := os.Getenv("MEDIA_MCP_CONFIG"); p != "" {
		cfgPath = p
	} else {
		// Try relative to executable, then current dir.
		exe, err := os.Executable()
		if err == nil {
			candidate := filepath.Join(filepath.Dir(exe), "config.yml")
			if _, err := os.Stat(candidate); err == nil {
				cfgPath = candidate
			}
		}
		if _, err := os.Stat(cfgPath); err != nil {
			// Fallback to current directory.
			cfgPath = filepath.Join(".", "config.yml")
		}
	}

	fmt.Fprintf(os.Stderr, "[media-mcp] Loading config from: %s\n", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// --- Dynamically create suppliers from config ---
	var imageSuppliers []supplier.ImageSupplier

	for name, sCfg := range cfg.Suppliers {
		if !sCfg.Enabled {
			fmt.Fprintf(os.Stderr, "[media-mcp] Supplier %q is disabled, skipping\n", name)
			continue
		}

		switch name {
		case "senseNova":
			adapter := &supplier.SenseNovaAdapter{
				BaseURL:       sCfg.BaseURL,
				APIKey:        sCfg.APIKey,
				Model:         defaultStr(sCfg.Model, "sensenova-u1-fast"),
				Size:          defaultStr(sCfg.Size, "2752x1536"),
				N:             maxInt(sCfg.Extra["n"], 1),
				CustomHeaders: sCfg.Headers,
				ExtraFields:   sCfg.Extra,
				Config:        &sCfg,
			}
			imageSuppliers = append(imageSuppliers, adapter)
			fmt.Fprintf(os.Stderr, "[media-mcp] Registered supplier: %s (%s)\n", adapter.Name(), sCfg.BaseURL)

		case "agnes_ai":
			adapter := supplier.NewAgnesAIAdapter(&sCfg)
			imageSuppliers = append(imageSuppliers, adapter)
			fmt.Fprintf(os.Stderr, "[media-mcp] Registered supplier: %s (%s)\n", adapter.Name(), sCfg.BaseURL)

		case "doubao_seedream":
			adapter := supplier.NewDoubaoSeedreamAdapter(name, &sCfg)
			imageSuppliers = append(imageSuppliers, adapter)
			fmt.Fprintf(os.Stderr, "[media-mcp] Registered supplier: %s (%s)\n", adapter.Name(), sCfg.BaseURL)

		default:
			// Generic HTTP-based adapter for any unknown supplier.
			adapter := supplier.NewHTTPGenericAdapter(name, &sCfg)
			if sCfg.SupplierType == "image" || sCfg.SupplierType == "both" {
				imageSuppliers = append(imageSuppliers, adapter)
				fmt.Fprintf(os.Stderr, "[media-mcp] Registered generic supplier: %s (%s)\n", name, sCfg.BaseURL)
			}
			if sCfg.SupplierType == "video" || sCfg.SupplierType == "both" {
				fmt.Fprintf(os.Stderr, "[media-mcp] Note: video support for %s is not yet implemented\n", name)
			}
		}
	}

	if len(imageSuppliers) == 0 {
		log.Fatal("No enabled suppliers found in config. Please check config.yml")
	}

	// Create MCP server and start listening on stdio.
	server := mcp.NewServer(cfg)
	server.RegisterSuppliers(imageSuppliers)

	fmt.Fprintf(os.Stderr, "[media-mcp] Starting MCP server with %d supplier(s) on stdio\n", len(imageSuppliers))
	if err := server.Start(); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func maxInt(a interface{}, def int) int {
	switch v := a.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return def
}
