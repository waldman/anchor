package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"time"
)

// Runner executes ansible-playbook on the local machine.
type Runner interface {
	Apply(ctx context.Context, treeDir, node, hostname string) (time.Duration, error)
}

type ansibleRunner struct {
	playbookBin string
}

func New(playbookBin string) Runner {
	return &ansibleRunner{playbookBin: playbookBin}
}

func (r *ansibleRunner) Apply(ctx context.Context, treeDir, node, hostname string) (time.Duration, error) {
	playbook := filepath.Join("nodes", filepath.FromSlash(node), "playbook.yml")

	cmd := exec.CommandContext(ctx, r.playbookBin,
		"-i", hostname+",",
		"-e", "ansible_connection=local",
		playbook,
	)
	cmd.Dir = treeDir

	start := time.Now()
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		slog.Error("ansible-playbook failed",
			"node", node,
			"duration_s", int(duration.Seconds()),
			"output", string(out),
			"error", err,
		)
		return duration, fmt.Errorf("ansible-playbook: %w", err)
	}

	slog.Info("ansible-playbook succeeded",
		"node", node,
		"duration_s", int(duration.Seconds()),
	)
	return duration, nil
}
