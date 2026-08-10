# 02 — Configuration

## File Location

`/etc/anchor/anchor.toml`

File permissions: `0600`, owned by the user the daemon runs as.

## Full Schema

```toml
[daemon]
node          = "home/production/acme_webserver"   # required — node path in S3 tree
poll_interval = "5m"                               # required — duration: 30s, 5m, 1h
working_dir   = "/var/lib/anchor"                  # optional — default: /var/lib/anchor

[s3]
# Bucket name. May encode a key prefix by including a '/':
#   "my-fleet-config"            → bucket only
#   "my-fleet-config/production" → bucket=my-fleet-config, prefix=production/
bucket = "my-fleet-config"   # required

[ansible]
playbook_bin = "ansible-playbook"   # optional — default: ansible-playbook (PATH lookup)

[aws]
region            = "us-east-1"   # required
# All optional. If absent, the default credential chain applies:
#   instance profile → env vars (AWS_ACCESS_KEY_ID etc.) → ~/.aws/credentials
access_key_id     = ""
secret_access_key = ""
profile           = ""            # named profile in ~/.aws/credentials

[state]
dynamodb_table = "fleet-state"    # required — DynamoDB table name (see 06_state.md)
ttl_days       = 15               # optional — default: 15

[log]
level  = "info"    # optional — debug / info / warn / error. Default: info
format = "json"    # optional — json / text. Default: json
```

## Field Reference

### `[daemon]`

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `node` | string | yes | — | Path within `nodes/` in the S3 tree |
| `poll_interval` | duration | yes | — | How often to poll `current`. Parsed as Go duration string. |
| `working_dir` | string | no | `/var/lib/anchor` | Where sha trees are downloaded |

### `[s3]`

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `bucket` | string | yes | — | S3 bucket, optionally including key prefix after first `/` |

### `[ansible]`

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `playbook_bin` | string | no | `ansible-playbook` | PATH lookup or absolute path |

### `[aws]`

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `region` | string | yes | — | AWS region for S3 and DynamoDB |
| `access_key_id` | string | no | `""` | Explicit credentials — non-AWS deployments |
| `secret_access_key` | string | no | `""` | Explicit credentials — non-AWS deployments |
| `profile` | string | no | `""` | Named profile in `~/.aws/credentials` |

**Credential priority:** explicit keys → named profile → default chain.

### `[state]`

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `dynamodb_table` | string | yes | — | DynamoDB table name |
| `ttl_days` | int | no | `15` | Days before a record is auto-purged by DynamoDB |

### `[log]`

| Field | Type | Required | Default | Notes |
|---|---|---|---|---|
| `level` | string | no | `info` | Log verbosity |
| `format` | string | no | `json` | `json` for log aggregation, `text` for human reading |

## Startup Validation

On startup, the daemon validates:
- `node` is non-empty
- `poll_interval` parses as a valid duration > 0
- `bucket` is non-empty
- `aws.region` is non-empty
- `state.dynamodb_table` is non-empty

Fatal error on any missing required field. No silent defaults for required fields.
