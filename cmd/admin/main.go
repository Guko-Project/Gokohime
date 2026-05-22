package main

import (
	"flag"

	log "github.com/sirupsen/logrus"

	"github.com/colanns/gokohime/internal/admin"
	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
)

func main() {
	configPath := flag.String("c", "config.yaml", "Path to config file")
	debug := flag.Bool("d", false, "Enable debug logging")
	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[admin] load config: %v", err)
	}

	db, err := database.InitWithDialect(cfg.Database.Driver(), cfg.Database.DSN())
	if err != nil {
		log.Fatalf("[admin] init database: %v", err)
	}

	server, err := admin.NewServer(cfg, db)
	if err != nil {
		log.Fatalf("[admin] create server: %v", err)
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("[admin] server stopped: %v", err)
	}
}
