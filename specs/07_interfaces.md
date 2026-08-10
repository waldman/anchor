# 07 — Go Interfaces

## Purpose

These interfaces define the contracts between Anchor's components. Each boundary
gets a Go interface; implementations are injected at startup. Tests use mock
implementations of these interfaces.

## `S3Client`

```go
type S3Client interface {
    // GetObject returns nil, nil for a 404 (key not found).
    GetObject(ctx context.Context, key string) ([]byte, error)

    // DownloadTree copies all keys under prefix into destDir.
    DownloadTree(ctx context.Context, prefix string, destDir string) error
}
```

Real implementation: `aws-sdk-go-v2` S3 client.
Mock implementation: in-memory map for unit tests.

## `Runner`

```go
type Runner interface {
    // Apply runs ansible-playbook for the given node within treeDir.
    // Returns the duration and any error (non-zero exit code).
    Apply(ctx context.Context, treeDir string, node string, hostname string) (time.Duration, error)
}
```

Real implementation: `os/exec` subprocess.
Mock implementation: configurable success/failure with fixed duration.

## `StateStore`

```go
type StateStore interface {
    // Write records the result of an apply event.
    Write(ctx context.Context, record StateRecord) error
}

type StateRecord struct {
    Hostname      string
    Node          string
    SHA           string
    Status        string        // "success" / "failed" / "skipped"
    ApplyTime     time.Time
    DurationS     int
    DaemonVersion string
    Error         string
}
```

Real implementation: writes to both `state.json` and DynamoDB.
Mock implementation: captures records for assertion in tests.

## `Config`

Not an interface — a plain struct loaded once at startup:

```go
type Config struct {
    Daemon  DaemonConfig
    S3      S3Config
    Ansible AnsibleConfig
    AWS     AWSConfig
    State   StateConfig
    Log     LogConfig
}

type DaemonConfig struct {
    Node         string
    PollInterval time.Duration
    WorkingDir   string
}

type S3Config struct {
    Bucket string   // raw field value — may include prefix after first '/'
    BucketName string  // parsed: before first '/'
    KeyPrefix  string  // parsed: after first '/', empty if no '/'
}

type AWSConfig struct {
    Region          string
    AccessKeyID     string
    SecretAccessKey string
    Profile         string
}

type StateConfig struct {
    DynamoDBTable string
    TTLDays       int
}

type LogConfig struct {
    Level  string
    Format string
}
```

## Wiring

`main.go` builds concrete implementations and wires them together:

```go
s3Client  := s3.NewAWSClient(cfg)
runner    := runner.NewAnsibleRunner(cfg)
store     := state.NewCompositeStore(cfg)   // local + DynamoDB

daemon := daemon.New(cfg, s3Client, runner, store)
daemon.Run(ctx)
```

The daemon core (`daemon.New`) depends only on the interfaces — not on any
concrete implementation. This makes the core fully testable with mocks.
