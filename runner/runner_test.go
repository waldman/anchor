package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%q not found in PATH: %v", name, err)
	}
	return path
}

func TestAnsibleRunner_Success(t *testing.T) {
	dir := t.TempDir()
	r := New(mustLookPath(t, "true"))
	duration, err := r.Apply(context.Background(), dir, "home/production/web", "web-01.example.com")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if duration < 0 {
		t.Errorf("duration should be >= 0, got %v", duration)
	}
}

func TestAnsibleRunner_Failure(t *testing.T) {
	dir := t.TempDir()
	r := New(mustLookPath(t, "false"))
	_, err := r.Apply(context.Background(), dir, "home/production/web", "web-01.example.com")
	if err == nil {
		t.Fatal("expected error from false, got nil")
	}
}

func TestAnsibleRunner_WorkingDir(t *testing.T) {
	// Verify the runner uses treeDir as working directory.
	// The script prints its cwd; we verify it matches treeDir.
	treeDir := t.TempDir()

	script := filepath.Join(t.TempDir(), "check-cwd.sh")
	// Script exits 0 only if $PWD == treeDir
	content := "#!/bin/sh\n[ \"$PWD\" = \"" + treeDir + "\" ]\n"
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}

	r := New(script)
	_, err := r.Apply(context.Background(), treeDir, "home/prod/web", "host-01")
	if err != nil {
		t.Fatalf("expected cwd to be treeDir, got error: %v", err)
	}
}

func TestAnsibleRunner_NotFound(t *testing.T) {
	r := New("/does/not/exist/ansible-playbook")
	_, err := r.Apply(context.Background(), t.TempDir(), "home/prod/web", "host-01")
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}
