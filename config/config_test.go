package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "anchor-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

const validConfig = `
[daemon]
node          = "home/production/web"
poll_interval = "5m"

[s3]
bucket = "my-bucket"

[aws]
region = "us-east-1"

[state]
dynamodb_table = "fleet-state"
`

func TestLoad_Valid(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, validConfig))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Daemon.Node != "home/production/web" {
		t.Errorf("node = %q, want %q", cfg.Daemon.Node, "home/production/web")
	}
	if cfg.Daemon.PollInterval != 5*time.Minute {
		t.Errorf("poll_interval = %v, want 5m", cfg.Daemon.PollInterval)
	}
	if cfg.S3.BucketName != "my-bucket" {
		t.Errorf("bucket name = %q, want %q", cfg.S3.BucketName, "my-bucket")
	}
	if cfg.AWS.Region != "us-east-1" {
		t.Errorf("region = %q, want %q", cfg.AWS.Region, "us-east-1")
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, validConfig))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Daemon.WorkingDir != "/var/lib/anchor" {
		t.Errorf("working_dir = %q, want /var/lib/anchor", cfg.Daemon.WorkingDir)
	}
	if cfg.Ansible.PlaybookBin != "ansible-playbook" {
		t.Errorf("playbook_bin = %q, want ansible-playbook", cfg.Ansible.PlaybookBin)
	}
	if cfg.State.TTLDays != 15 {
		t.Errorf("ttl_days = %d, want 15", cfg.State.TTLDays)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log.level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("log.format = %q, want json", cfg.Log.Format)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{
			name: "missing node",
			toml: `
[daemon]
poll_interval = "5m"
[s3]
bucket = "b"
[aws]
region = "us-east-1"
[state]
dynamodb_table = "t"`,
			wantErr: "daemon.node",
		},
		{
			name: "missing poll_interval",
			toml: `
[daemon]
node = "home/prod/web"
[s3]
bucket = "b"
[aws]
region = "us-east-1"
[state]
dynamodb_table = "t"`,
			wantErr: "daemon.poll_interval",
		},
		{
			name: "missing bucket",
			toml: `
[daemon]
node = "home/prod/web"
poll_interval = "5m"
[aws]
region = "us-east-1"
[state]
dynamodb_table = "t"`,
			wantErr: "s3.bucket",
		},
		{
			name: "missing region",
			toml: `
[daemon]
node = "home/prod/web"
poll_interval = "5m"
[s3]
bucket = "b"
[state]
dynamodb_table = "t"`,
			wantErr: "aws.region",
		},
		{
			name: "missing dynamodb_table",
			toml: `
[daemon]
node = "home/prod/web"
poll_interval = "5m"
[s3]
bucket = "b"
[aws]
region = "us-east-1"`,
			wantErr: "state.dynamodb_table",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTempConfig(t, tc.toml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseBucket(t *testing.T) {
	cases := []struct {
		bucket         string
		wantBucketName string
		wantKeyPrefix  string
	}{
		{"my-bucket", "my-bucket", ""},
		{"my-bucket/production", "my-bucket", "production/"},
		{"my-bucket/prod/eu-west-1", "my-bucket", "prod/eu-west-1/"},
	}
	for _, tc := range cases {
		t.Run(tc.bucket, func(t *testing.T) {
			cfg := &Config{S3: S3Config{Bucket: tc.bucket}}
			parseBucket(cfg)
			if cfg.S3.BucketName != tc.wantBucketName {
				t.Errorf("BucketName = %q, want %q", cfg.S3.BucketName, tc.wantBucketName)
			}
			if cfg.S3.KeyPrefix != tc.wantKeyPrefix {
				t.Errorf("KeyPrefix = %q, want %q", cfg.S3.KeyPrefix, tc.wantKeyPrefix)
			}
		})
	}
}

func TestLoad_BadTOML(t *testing.T) {
	_, err := Load(writeTempConfig(t, "this is not toml :::"))
	if err == nil {
		t.Fatal("expected error for bad TOML, got nil")
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	_, err := Load("/does/not/exist/anchor.toml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
