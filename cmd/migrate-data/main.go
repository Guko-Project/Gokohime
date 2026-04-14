package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/migrator"
)

func main() {
	configPath := flag.String("c", "config.yaml", "Path to config file")
	repoRoot := flag.String("root", ".", "Repository root path")
	mode := flag.String("mode", string(migrator.ModeAll), "Migration mode: all, archive-only, structured-only")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		exitf("load config: %v", err)
	}

	db, err := database.Init(cfg.Database.DSN())
	if err != nil {
		exitf("init database: %v", err)
	}

	absRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		exitf("resolve root: %v", err)
	}

	stats, err := migrator.RunWithOptions(context.Background(), db, absRoot, migrator.Options{
		Mode: migrator.Mode(*mode),
	})
	if err != nil {
		exitf("run migration: %v", err)
	}

	output, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		exitf("marshal stats: %v", err)
	}

	fmt.Println(string(output))
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
