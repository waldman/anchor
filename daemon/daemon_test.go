package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/waldman/anchor/config"
	"github.com/waldman/anchor/state"
)

// ── mocks ────────────────────────────────────────────────────────────────────

type mockS3 struct {
	objects map[string][]byte // nil value = 404
	err     error             // if set, all calls return this error
}

func (m *mockS3) GetObject(_ context.Context, key string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	v, ok := m.objects[key]
	if !ok {
		return nil, nil // 404
	}
	return v, nil
}

func (m *mockS3) DownloadTree(_ context.Context, prefix, destDir string) error {
	if m.err != nil {
		return m.err
	}
	for key, data := range m.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rel := strings.TrimPrefix(key, prefix)
		if rel == "" {
			continue
		}
		path := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

type mockRunner struct {
	err      error
	duration time.Duration
	calls    int
}

func (m *mockRunner) Apply(_ context.Context, _, _, _ string) (time.Duration, error) {
	m.calls++
	return m.duration, m.err
}

type mockStore struct {
	records []state.Record
}

func (m *mockStore) Write(_ context.Context, r state.Record) error {
	m.records = append(m.records, r)
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

const testNode = "home/production/web"
const testSHA = "abc1234def5678"

func newTestDaemon(t *testing.T, s3c *mockS3, r *mockRunner, store *mockStore) *Daemon {
	t.Helper()
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Node:         testNode,
			PollInterval: time.Minute,
			WorkingDir:   t.TempDir(),
		},
		Ansible: config.AnsibleConfig{PlaybookBin: "ansible-playbook"},
	}
	d, err := New(cfg, s3c, r, store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// s3WithTree builds a mock S3 that has `current` pointing to sha and a valid tree for node.
func s3WithTree(sha, node string, extra map[string][]byte) *mockS3 {
	objects := map[string][]byte{
		"current": []byte(sha),
		fmt.Sprintf("commits/%s/nodes/%s/playbook.yml", sha, node): []byte("---"),
	}
	for k, v := range extra {
		objects[k] = v
	}
	return &mockS3{objects: objects}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestRunOnce_NoChange(t *testing.T) {
	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, nil)

	d := newTestDaemon(t, s3c, runner, store)
	d.lastSHA = testSHA // already on this sha

	d.runOnce(context.Background())

	if runner.calls != 0 {
		t.Errorf("runner called %d times, want 0", runner.calls)
	}
	if len(store.records) != 0 {
		t.Errorf("state written %d times, want 0", len(store.records))
	}
}

func TestRunOnce_ApplySuccess(t *testing.T) {
	store := &mockStore{}
	runner := &mockRunner{duration: 2 * time.Second}
	s3c := s3WithTree(testSHA, testNode, nil)

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 1 {
		t.Fatalf("runner called %d times, want 1", runner.calls)
	}
	if len(store.records) != 1 {
		t.Fatalf("state written %d times, want 1", len(store.records))
	}
	r := store.records[0]
	if r.Status != "success" {
		t.Errorf("status = %q, want success", r.Status)
	}
	if r.SHA != testSHA {
		t.Errorf("sha = %q, want %q", r.SHA, testSHA)
	}
	if d.lastSHA != testSHA {
		t.Errorf("lastSHA = %q, want %q after success", d.lastSHA, testSHA)
	}
}

func TestRunOnce_ApplyFailed(t *testing.T) {
	store := &mockStore{}
	runner := &mockRunner{err: fmt.Errorf("playbook error")}
	s3c := s3WithTree(testSHA, testNode, nil)

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 1 {
		t.Fatalf("runner called %d times, want 1", runner.calls)
	}
	if len(store.records) != 1 {
		t.Fatalf("state written %d times, want 1", len(store.records))
	}
	r := store.records[0]
	if r.Status != "failed" {
		t.Errorf("status = %q, want failed", r.Status)
	}
	if d.lastSHA == testSHA {
		t.Errorf("lastSHA should not be updated after failed apply")
	}
	if r.Error == "" {
		t.Errorf("error field should be set on failed apply")
	}
}

func TestRunOnce_CanaryAbsent_AllApply(t *testing.T) {
	// No canary.txt → everyone applies
	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, nil)

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 1 {
		t.Errorf("runner called %d times, want 1 (no canary = apply)", runner.calls)
	}
}

func TestRunOnce_CanaryHostnameMatch_Applies(t *testing.T) {
	hostname, _ := os.Hostname()
	canaryContent := fmt.Sprintf("# comment\n%s\nother-host.example.com\n", hostname)
	canaryKey := fmt.Sprintf("commits/%s/nodes/%s/canary.txt", testSHA, testNode)

	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, map[string][]byte{
		canaryKey: []byte(canaryContent),
	})

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 1 {
		t.Errorf("runner called %d times, want 1 (hostname listed in canary)", runner.calls)
	}
	if store.records[0].Status != "success" {
		t.Errorf("status = %q, want success", store.records[0].Status)
	}
}

func TestRunOnce_CanaryHostnameNotListed_Skips(t *testing.T) {
	canaryKey := fmt.Sprintf("commits/%s/nodes/%s/canary.txt", testSHA, testNode)

	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, map[string][]byte{
		canaryKey: []byte("other-host-01.example.com\nother-host-02.example.com\n"),
	})

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 0 {
		t.Errorf("runner called %d times, want 0 (hostname not in canary)", runner.calls)
	}
	if len(store.records) != 1 {
		t.Fatalf("state written %d times, want 1", len(store.records))
	}
	if store.records[0].Status != "skipped" {
		t.Errorf("status = %q, want skipped", store.records[0].Status)
	}
}

func TestRunOnce_CanaryEmpty_AllSkip(t *testing.T) {
	canaryKey := fmt.Sprintf("commits/%s/nodes/%s/canary.txt", testSHA, testNode)

	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, map[string][]byte{
		canaryKey: []byte(""),
	})

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 0 {
		t.Errorf("runner called %d times, want 0 (empty canary = no one applies)", runner.calls)
	}
	if store.records[0].Status != "skipped" {
		t.Errorf("status = %q, want skipped", store.records[0].Status)
	}
}

func TestRunOnce_CanaryCommentsAndBlanks_Ignored(t *testing.T) {
	hostname, _ := os.Hostname()
	canaryKey := fmt.Sprintf("commits/%s/nodes/%s/canary.txt", testSHA, testNode)
	content := fmt.Sprintf("\n# this is a comment\n\n%s\n\n", hostname)

	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, map[string][]byte{
		canaryKey: []byte(content),
	})

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 1 {
		t.Errorf("runner called %d times, want 1 (hostname in canary, blanks+comments ignored)", runner.calls)
	}
}

func TestRunOnce_CanaryPartialMatch_Rejected(t *testing.T) {
	// "web-01" should not match "web-01.example.com" — exact match only
	hostname, _ := os.Hostname()
	shortName := strings.Split(hostname, ".")[0]
	if shortName == hostname {
		t.Skip("hostname has no dot — partial match test not applicable")
	}

	canaryKey := fmt.Sprintf("commits/%s/nodes/%s/canary.txt", testSHA, testNode)
	store := &mockStore{}
	runner := &mockRunner{}
	s3c := s3WithTree(testSHA, testNode, map[string][]byte{
		canaryKey: []byte(shortName + "\n"), // short name only, not FQDN
	})

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if runner.calls != 0 {
		t.Errorf("runner called %d times, want 0 (partial hostname must not match)", runner.calls)
	}
}

func TestRunOnce_S3PollError(t *testing.T) {
	store := &mockStore{}
	runner := &mockRunner{}
	s3c := &mockS3{err: fmt.Errorf("connection refused")}

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background()) // must not panic

	if runner.calls != 0 {
		t.Errorf("runner called %d times, want 0 on S3 error", runner.calls)
	}
}

func TestRunOnce_CurrentNotFound(t *testing.T) {
	store := &mockStore{}
	runner := &mockRunner{}
	s3c := &mockS3{objects: map[string][]byte{}} // no "current" key

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background()) // must not panic

	if runner.calls != 0 {
		t.Errorf("runner called %d times, want 0 when current missing", runner.calls)
	}
}

func TestRunOnce_StateRecordFields(t *testing.T) {
	store := &mockStore{}
	runner := &mockRunner{duration: 5 * time.Second}
	s3c := s3WithTree(testSHA, testNode, nil)

	d := newTestDaemon(t, s3c, runner, store)
	d.runOnce(context.Background())

	if len(store.records) == 0 {
		t.Fatal("no state records written")
	}
	r := store.records[0]
	if r.Hostname == "" {
		t.Error("hostname should not be empty")
	}
	if r.Node != testNode {
		t.Errorf("node = %q, want %q", r.Node, testNode)
	}
	if r.DaemonVersion != Version {
		t.Errorf("daemon_version = %q, want %q", r.DaemonVersion, Version)
	}
	if r.ApplyTime.IsZero() {
		t.Error("apply_time should not be zero")
	}
}
