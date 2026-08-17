package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/waldman/anchor/config"
	"github.com/waldman/anchor/daemon"
	"github.com/waldman/anchor/runner"
	anchors3 "github.com/waldman/anchor/s3"
	"github.com/waldman/anchor/state"
)

func main() {
	cfgPath := flag.String("config", "/etc/anchor/anchor.toml", "path to config file")
	daemonMode := flag.Bool("daemon", false, "run continuously, polling on an interval (for systemd)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "anchor: %v\n", err)
		os.Exit(1)
	}

	setupLogging(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	awscfg, err := cfg.BuildAWSConfig(ctx)
	if err != nil {
		slog.Error("failed to build AWS config", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.Daemon.WorkingDir, 0755); err != nil {
		slog.Error("failed to create working directory", "error", err)
		os.Exit(1)
	}

	s3client := anchors3.New(awscfg, cfg)
	store := state.NewCompositeStore(awscfg, cfg, cfg.Daemon.WorkingDir)
	r := runner.New(cfg.Ansible.PlaybookBin, cfg.Secrets.Prefix)

	d, err := daemon.New(cfg, s3client, r, store)
	if err != nil {
		slog.Error("failed to create daemon", "error", err)
		os.Exit(1)
	}

	if *daemonMode {
		d.Run(ctx)
	} else {
		if err := d.RunOnce(ctx); err != nil {
			os.Exit(1)
		}
	}
}

func setupLogging(cfg *config.Config) {
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Log.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}
