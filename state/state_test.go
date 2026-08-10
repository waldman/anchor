package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalStore_Write(t *testing.T) {
	dir := t.TempDir()
	store := newLocalStore(dir)

	applyTime := time.Date(2026, 8, 10, 14, 23, 1, 0, time.UTC)
	r := Record{
		Hostname:      "web-01.example.com",
		Node:          "home/production/acme_webserver",
		SHA:           "abc1234def5678",
		Status:        "success",
		ApplyTime:     applyTime,
		DurationS:     47,
		DaemonVersion: "0.1.0",
		Error:         "",
	}

	if err := store.Write(context.Background(), r); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}

	var got jsonRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal state.json: %v", err)
	}

	if got.Hostname != r.Hostname {
		t.Errorf("hostname = %q, want %q", got.Hostname, r.Hostname)
	}
	if got.Node != r.Node {
		t.Errorf("node = %q, want %q", got.Node, r.Node)
	}
	if got.SHA != r.SHA {
		t.Errorf("sha = %q, want %q", got.SHA, r.SHA)
	}
	if got.Status != r.Status {
		t.Errorf("status = %q, want %q", got.Status, r.Status)
	}
	if got.ApplyTime != "2026-08-10T14:23:01Z" {
		t.Errorf("apply_time = %q, want %q", got.ApplyTime, "2026-08-10T14:23:01Z")
	}
	if got.DurationS != r.DurationS {
		t.Errorf("duration_s = %d, want %d", got.DurationS, r.DurationS)
	}
	if got.DaemonVersion != r.DaemonVersion {
		t.Errorf("daemon_version = %q, want %q", got.DaemonVersion, r.DaemonVersion)
	}
}

func TestLocalStore_Write_Overwrites(t *testing.T) {
	dir := t.TempDir()
	store := newLocalStore(dir)
	ctx := context.Background()

	r1 := Record{Hostname: "web-01", SHA: "aaa", Status: "success", ApplyTime: time.Now()}
	r2 := Record{Hostname: "web-01", SHA: "bbb", Status: "failed", ApplyTime: time.Now(), Error: "boom"}

	if err := store.Write(ctx, r1); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(ctx, r2); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	var got jsonRecord
	json.Unmarshal(data, &got)

	if got.SHA != "bbb" {
		t.Errorf("sha = %q, want bbb (second write should overwrite)", got.SHA)
	}
	if got.Error != "boom" {
		t.Errorf("error = %q, want boom", got.Error)
	}
}

func TestLocalStore_Write_BadDir(t *testing.T) {
	store := newLocalStore("/does/not/exist")
	err := store.Write(context.Background(), Record{ApplyTime: time.Now()})
	if err == nil {
		t.Fatal("expected error writing to nonexistent dir, got nil")
	}
}
