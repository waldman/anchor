package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/waldman/anchor/config"
	"github.com/waldman/anchor/runner"
	"github.com/waldman/anchor/s3"
	"github.com/waldman/anchor/state"
)

const Version = "0.2.0"

type Daemon struct {
	cfg      *config.Config
	s3       s3.Client
	runner   runner.Runner
	store    state.Store
	hostname string
	lastSHA  string
}

func New(cfg *config.Config, s3c s3.Client, r runner.Runner, store state.Store) (*Daemon, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("get hostname: %w", err)
	}
	return &Daemon{
		cfg:      cfg,
		s3:       s3c,
		runner:   r,
		store:    store,
		hostname: hostname,
	}, nil
}

// RunOnce polls S3 once, applies if the sha has changed, and returns.
// Returns an error if the apply failed or S3 was unreachable.
// A skipped apply (canary or sha unchanged) returns nil.
func (d *Daemon) RunOnce(ctx context.Context) error {
	return d.runOnce(ctx)
}

// Run polls S3 on each tick and applies when the sha changes.
// Blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) {
	slog.Info("anchor starting",
		"version", Version,
		"node", d.cfg.Daemon.Node,
		"hostname", d.hostname,
		"poll_interval", d.cfg.Daemon.PollInterval,
	)
	_ = d.runOnce(ctx)
	for {
		select {
		case <-time.After(d.cfg.Daemon.PollInterval):
			_ = d.runOnce(ctx)
		case <-ctx.Done():
			slog.Info("anchor stopping")
			return
		}
	}
}

func (d *Daemon) runOnce(ctx context.Context) error {
	sha, err := d.pollCurrent(ctx)
	if err != nil {
		slog.Error("poll failed", "error", err)
		return err
	}
	if sha == d.lastSHA {
		slog.Debug("no change", "sha", sha)
		return nil
	}

	slog.Info("new sha detected", "sha", sha, "previous", d.lastSHA)

	apply, err := d.checkCanary(ctx, sha)
	if err != nil {
		slog.Error("canary check error, skipping apply", "sha", sha, "error", err)
		d.writeState(ctx, state.Record{
			Hostname: d.hostname, Node: d.cfg.Daemon.Node, SHA: sha,
			Status: "skipped", ApplyTime: time.Now().UTC(), DaemonVersion: Version,
		})
		return nil
	}
	if !apply {
		slog.Info("canary: sha not for this host, skipping", "sha", sha, "hostname", d.hostname)
		d.writeState(ctx, state.Record{
			Hostname: d.hostname, Node: d.cfg.Daemon.Node, SHA: sha,
			Status: "skipped", ApplyTime: time.Now().UTC(), DaemonVersion: Version,
		})
		return nil
	}

	treeDir, err := d.fetchTree(ctx, sha)
	if err != nil {
		slog.Error("fetch tree failed", "sha", sha, "error", err)
		return err
	}

	duration, applyErr := d.runner.Apply(ctx, treeDir, d.cfg.Daemon.Node, d.hostname)
	now := time.Now().UTC()

	if applyErr != nil {
		slog.Error("apply failed", "sha", sha, "error", applyErr)
		d.writeState(ctx, state.Record{
			Hostname: d.hostname, Node: d.cfg.Daemon.Node, SHA: sha,
			Status: "failed", ApplyTime: now, DurationS: int(duration.Seconds()),
			DaemonVersion: Version, Error: applyErr.Error(),
		})
		return applyErr
	}

	slog.Info("apply succeeded", "sha", sha, "duration_s", int(duration.Seconds()))
	d.lastSHA = sha
	d.writeState(ctx, state.Record{
		Hostname: d.hostname, Node: d.cfg.Daemon.Node, SHA: sha,
		Status: "success", ApplyTime: now, DurationS: int(duration.Seconds()),
		DaemonVersion: Version,
	})
	return nil
}

func (d *Daemon) pollCurrent(ctx context.Context) (string, error) {
	data, err := d.s3.GetObject(ctx, "current")
	if err != nil {
		return "", fmt.Errorf("read current: %w", err)
	}
	if data == nil {
		return "", fmt.Errorf("current file not found in S3")
	}
	return strings.TrimSpace(string(data)), nil
}

func (d *Daemon) checkCanary(ctx context.Context, sha string) (bool, error) {
	key := fmt.Sprintf("commits/%s/canary/%s.txt", sha, d.cfg.Daemon.Node)
	data, err := d.s3.GetObject(ctx, key)
	if err != nil {
		return false, err
	}
	if data == nil {
		return true, nil // absent → everyone applies
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == d.hostname {
			return true, nil
		}
	}
	return false, nil // not listed (or empty file)
}

func (d *Daemon) fetchTree(ctx context.Context, sha string) (string, error) {
	stagingDir := filepath.Join(d.cfg.Daemon.WorkingDir, "staging", sha)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}

	prefix := fmt.Sprintf("commits/%s/", sha)
	if err := d.s3.DownloadTree(ctx, prefix, stagingDir); err != nil {
		os.RemoveAll(stagingDir)
		return "", fmt.Errorf("download tree: %w", err)
	}

	playbook := filepath.Join(stagingDir, "nodes", filepath.FromSlash(d.cfg.Daemon.Node)+".yml")
	if _, err := os.Stat(playbook); err != nil {
		os.RemoveAll(stagingDir)
		return "", fmt.Errorf("node playbook not found in tree: nodes/%s.yml", d.cfg.Daemon.Node)
	}

	// Write inventory file for Ansible host_vars discovery.
	// A file inventory (vs inline "-i host,") lets Ansible resolve
	// host_vars/<hostname>/ relative to inventory_dir (the tree root).
	invPath := filepath.Join(stagingDir, "inventory")
	if err := os.WriteFile(invPath, []byte(d.hostname+"\n"), 0644); err != nil {
		os.RemoveAll(stagingDir)
		return "", fmt.Errorf("write inventory: %w", err)
	}

	currentDir := filepath.Join(d.cfg.Daemon.WorkingDir, "current")
	os.RemoveAll(currentDir)
	if err := os.Rename(stagingDir, currentDir); err != nil {
		os.RemoveAll(stagingDir)
		return "", fmt.Errorf("activate tree: %w", err)
	}
	return currentDir, nil
}

func (d *Daemon) writeState(ctx context.Context, r state.Record) {
	if err := d.store.Write(ctx, r); err != nil {
		slog.Error("state write failed", "error", err)
	}
}
