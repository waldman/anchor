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
	playbookBin  string
	secretPrefix string
}

// New builds a Runner. secretPrefix is passed to ansible-playbook as the
// anchor_secret_prefix extra-var when non-empty. See specs/05_runner.md and
// specs/08_secrets.md.
func New(playbookBin, secretPrefix string) Runner {
	return &ansibleRunner{
		playbookBin:  playbookBin,
		secretPrefix: secretPrefix,
	}
}

func (r *ansibleRunner) Apply(ctx context.Context, treeDir, node, hostname string) (time.Duration, error) {
	playbook := filepath.Join("nodes", filepath.FromSlash(node)+".yml")

	args := []string{
		"-i", "inventory",
		"-e", "ansible_connection=local",
		"-e", "anchor_node=" + node,
	}
	if r.secretPrefix != "" {
		args = append(args, "-e", "anchor_secret_prefix="+r.secretPrefix)
	}
	args = append(args, playbook)

	cmd := exec.CommandContext(ctx, r.playbookBin, args...)
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
