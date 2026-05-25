package main

import (
	"context"
	"flag"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/colanns/gokohime/internal/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/driver"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
	randpic "github.com/colanns/gokohime/plugin/randpic"

	// Import all plugins (self-registering via init())
	_ "github.com/colanns/gokohime/plugin/banana"
	_ "github.com/colanns/gokohime/plugin/chp"
	_ "github.com/colanns/gokohime/plugin/commandlog"
	_ "github.com/colanns/gokohime/plugin/cp"
	_ "github.com/colanns/gokohime/plugin/eatwhat"
	_ "github.com/colanns/gokohime/plugin/guesssong"
	_ "github.com/colanns/gokohime/plugin/help"
	_ "github.com/colanns/gokohime/plugin/jrluck"
	_ "github.com/colanns/gokohime/plugin/kk"
	_ "github.com/colanns/gokohime/plugin/repeater"
	_ "github.com/colanns/gokohime/plugin/saying"
	_ "github.com/colanns/gokohime/plugin/stickersaver"
	_ "github.com/colanns/gokohime/plugin/tempban"
)

func main() {
	configPath := flag.String("c", "config.yaml", "Path to config file")
	debug := flag.Bool("d", false, "Enable debug logging")
	flag.Parse()

	log.Init(*debug)

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[main] load config: %v", err)
	}
	log.Info("[main] config loaded")

	// Init database
	_, err = database.InitWithDialect(cfg.Database.Driver(), cfg.Database.DSN())
	if err != nil {
		log.Fatalf("[main] init database: %v", err)
	}
	log.Info("[main] database initialized")

	if err := randpic.StartupCheck(context.Background()); err != nil {
		log.Warnf("[main] randpic startup check failed: %v", err)
	}

	// Pre-check that the WebSocket listen address is available
	wsAddr := cfg.Bot.RWSURL
	if u, err := url.Parse(wsAddr); err == nil && u.Host != "" {
		wsAddr = u.Host
	}
	if ln, err := net.Listen("tcp", wsAddr); err != nil {
		log.Fatalf("[main] WebSocket listen address unavailable: %v", err)
	} else {
		ln.Close()
	}

	// Configure ZeroBot
	zeroCfg := zero.Config{
		NickName:       []string{cfg.Bot.Nickname},
		CommandPrefix:  cfg.Bot.CommandPrefix,
		SuperUsers:     cfg.Bot.SuperUsers,
		RingLen:        cfg.Bot.RingLen,
		Latency:        time.Duration(cfg.Bot.LatencyMS) * time.Millisecond,
		MaxProcessTime: time.Duration(cfg.Bot.MaxProcessTimeMin) * time.Minute,
		Driver: []zero.Driver{
			driver.NewWebSocketServer(16, cfg.Bot.RWSURL, cfg.Bot.AccessToken),
		},
	}

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("[main] shutting down...")
		os.Exit(0)
	}()

	// Start ZeroBot
	log.Infof("[main] starting Gokohime bot (nickname=%s, prefix=%s)", cfg.Bot.Nickname, cfg.Bot.CommandPrefix)
	zero.RunAndBlock(&zeroCfg, func() {
		log.Info("[main] ZeroBot connected")
	})
}
