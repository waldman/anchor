package config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

type Config struct {
	Daemon  DaemonConfig  `toml:"daemon"`
	S3      S3Config      `toml:"s3"`
	Ansible AnsibleConfig `toml:"ansible"`
	AWS     AWSConfig     `toml:"aws"`
	State   StateConfig   `toml:"state"`
	Log     LogConfig     `toml:"log"`
}

type DaemonConfig struct {
	Node         string        `toml:"node"`
	PollInterval time.Duration `toml:"poll_interval"`
	WorkingDir   string        `toml:"working_dir"`
}

type S3Config struct {
	Bucket     string `toml:"bucket"`
	BucketName string // parsed from Bucket
	KeyPrefix  string // parsed from Bucket, includes trailing slash if non-empty
}

type AnsibleConfig struct {
	PlaybookBin string `toml:"playbook_bin"`
}

type AWSConfig struct {
	Region          string `toml:"region"`
	AccessKeyID     string `toml:"access_key_id"`
	SecretAccessKey string `toml:"secret_access_key"`
	Profile         string `toml:"profile"`
}

type StateConfig struct {
	DynamoDBTable string `toml:"dynamodb_table"`
	TTLDays       int    `toml:"ttl_days"`
}

type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	applyDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	parseBucket(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Daemon.WorkingDir == "" {
		cfg.Daemon.WorkingDir = "/var/lib/anchor"
	}
	if cfg.Ansible.PlaybookBin == "" {
		cfg.Ansible.PlaybookBin = "ansible-playbook"
	}
	if cfg.State.TTLDays == 0 {
		cfg.State.TTLDays = 15
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Format == "" {
		cfg.Log.Format = "json"
	}
}

func validate(cfg *Config) error {
	if cfg.Daemon.Node == "" {
		return fmt.Errorf("daemon.node is required")
	}
	if cfg.Daemon.PollInterval <= 0 {
		return fmt.Errorf("daemon.poll_interval must be > 0")
	}
	if cfg.S3.Bucket == "" {
		return fmt.Errorf("s3.bucket is required")
	}
	if cfg.AWS.Region == "" {
		return fmt.Errorf("aws.region is required")
	}
	if cfg.State.DynamoDBTable == "" {
		return fmt.Errorf("state.dynamodb_table is required")
	}
	return nil
}

func parseBucket(cfg *Config) {
	parts := strings.SplitN(cfg.S3.Bucket, "/", 2)
	cfg.S3.BucketName = parts[0]
	if len(parts) == 2 && parts[1] != "" {
		cfg.S3.KeyPrefix = parts[1] + "/"
	}
}

// BuildAWSConfig constructs an aws.Config from the AWS section of the config.
// Credential priority: explicit keys → named profile → default chain.
func (c *Config) BuildAWSConfig(ctx context.Context) (aws.Config, error) {
	opts := []func(*awscfg.LoadOptions) error{
		awscfg.WithRegion(c.AWS.Region),
	}
	if c.AWS.AccessKeyID != "" && c.AWS.SecretAccessKey != "" {
		opts = append(opts, awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				c.AWS.AccessKeyID,
				c.AWS.SecretAccessKey,
				"",
			),
		))
	} else if c.AWS.Profile != "" {
		opts = append(opts, awscfg.WithSharedConfigProfile(c.AWS.Profile))
	}
	built, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("build aws config: %w", err)
	}
	return built, nil
}
