# Anchor

Pull-based configuration management daemon. Anchor polls an S3 bucket for a
desired-state Ansible tree, downloads it when the sha changes, and applies it
locally via `ansible-playbook`. No master server. No proprietary DSL. No lock-in.

```
anchor -config /etc/anchor/anchor.toml
```

---

## How it works

Anchor follows a three-layer taxonomy:

| Layer | Directory | What it is |
|---|---|---|
| **Role** | `roles/` | Reusable Ansible role. One technology (`nginx`, `postgresql`). |
| **Playbook** | `playbooks/` | Business-layer composition. Assembles roles for a purpose. |
| **Node** | `nodes/<node>.yml` | Machine identity. One file per machine — includes + variables only, no per-node implementation. |

Operators push a new tree to S3 under `commits/<sha>/`, then write the sha to
`current`. On the next poll, every daemon that sees the change downloads the
tree and applies the node's file.

---

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/waldman/anchor/master/install.sh | bash \
  -s -- \
  --bucket my-fleet-config \
  --node home/production/web_server \
  --region us-east-1
```

Or download a binary directly from [releases](https://github.com/waldman/anchor/releases).

See [install.sh options](#installsh-options) for the full reference.

---

## Quick start

### 1. Create an S3 bucket

```bash
aws s3 mb s3://my-fleet-config
```

### 2. Upload a tree

See [`s3-bucket-example/`](s3-bucket-example/) for a minimal working example.

```bash
SHA=$(git rev-parse --short HEAD)
aws s3 sync s3-bucket-example/commits/example-sha-001/ s3://my-fleet-config/commits/$SHA/
echo "$SHA" | aws s3 cp - s3://my-fleet-config/current
```

### 3. Configure a machine

```toml
# /etc/anchor/anchor.toml
[daemon]
node          = "home/production/web_server"
poll_interval = "5m"

[s3]
bucket = "my-fleet-config"

[aws]
region = "us-east-1"

[state]
dynamodb_table = "fleet-state"
```

### 4. Run

```bash
# Apply once (like puppet agent -t)
anchor -config /etc/anchor/anchor.toml

# Run as a daemon (for systemd)
anchor -config /etc/anchor/anchor.toml --daemon
```

---

## Configuration

Full reference: [`anchor.toml.example`](anchor.toml.example)

| Field | Required | Default | Description |
|---|---|---|---|
| `daemon.node` | yes | — | Node path in S3 tree (`home/production/web_server`) |
| `daemon.poll_interval` | yes | — | Poll interval (`30s`, `5m`, `1h`) |
| `daemon.working_dir` | no | `/var/lib/anchor` | Where sha trees are downloaded |
| `s3.bucket` | yes | — | S3 bucket, optionally with key prefix (`my-bucket/prod`) |
| `aws.region` | yes | — | AWS region |
| `aws.access_key_id` | no | — | Explicit credentials (omit to use instance profile) |
| `aws.secret_access_key` | no | — | Explicit credentials |
| `aws.profile` | no | — | Named AWS profile |
| `state.dynamodb_table` | yes | — | DynamoDB table name for fleet state |
| `state.ttl_days` | no | `15` | Days before stale records are purged |
| `secrets.prefix` | no | — | Passed to Ansible as `anchor_secret_prefix`. See [Secrets](#secrets). |
| `log.level` | no | `info` | `debug` / `info` / `warn` / `error` |
| `log.format` | no | `json` | `json` / `text` |

### S3 bucket with key prefix

```toml
bucket = "my-fleet-config/production"
# resolves to: bucket=my-fleet-config, prefix=production/
```

### Credentials

Anchor uses the standard AWS credential chain by default (IAM instance profile →
environment variables → `~/.aws/credentials`). For non-AWS environments, set
`access_key_id` + `secret_access_key` or `profile` in `[aws]`.

---

## S3 layout

```
s3://<bucket>/
  current                              # active sha — flip this to deploy
  commits/
    <sha>/
      ansible.cfg                      # roles_path = roles:playbooks
      roles/
        <role-name>/                   # standard Ansible role structure
      playbooks/
        <playbook-name>/               # assembles roles for a business purpose
          tasks/main.yml
      nodes/
        <location>/
          <environment>/
            <name>.yml                 # node = single file: imports + vars
      canary/
        <location>/
          <environment>/
            <name>.txt                 # optional — controls who gets this sha
      host_vars/
        <hostname>/
          main.yml                     # per-hostname variable overrides
```

Three concerns, three trees. `nodes/` is pure identity, `canary/` is release
metadata, `host_vars/` is per-machine data.

### Canary deployments

Create `canary/<node>.txt` before flipping `current`:

```
# canary.txt — one hostname per line, exact match
web-01.example.com
web-02.example.com
```

Absent = all machines apply. Empty = no machines apply (emergency block).

### Host-specific variables

Place variable overrides in `host_vars/<hostname>/main.yml` at the tree root.
Variables only — no tasks, no role includes. Ansible resolves these automatically
because Anchor writes an inventory file containing the real hostname at fetch time.

---

## Fleet state (DynamoDB)

Anchor writes one record per machine to DynamoDB after every apply:

```
hostname        (PK)   web-01.example.com
node                   home/production/web_server
sha                    abc1234def5678
status                 success / failed / skipped
apply_time             2026-08-10T14:23:01Z
duration_s             47
daemon_version         0.1.0
error                  (empty unless failed)
ttl                    (auto-purged after ttl_days)
```

Create the table before first use:

```bash
aws dynamodb create-table \
  --table-name fleet-state \
  --attribute-definitions AttributeName=hostname,AttributeType=S \
  --key-schema AttributeName=hostname,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST \
  --region us-east-1
```

Enable TTL on the `ttl` attribute in the AWS console or via CLI.

---

## Secrets

Anchor does not fetch or manage secrets. Instead, it wires Ansible for
first-class secret retrieval via `amazon.aws.aws_secret` lookups.

Two extra-vars are injected into every `ansible-playbook` invocation:

| Var | Present when | Value |
|---|---|---|
| `anchor_node` | always | `[daemon].node` |
| `anchor_secret_prefix` | `[secrets].prefix` is set | `[secrets].prefix` |

The recommended convention: **one Secrets Manager secret per node**, named
`<prefix>/<anchor_node>`, valued as a flat JSON object of `UPPER_SNAKE_CASE`
string keys.

```yaml
- name: Load anchor secret bundle
  ansible.builtin.set_fact:
    _anchor_secrets: "{{ lookup('amazon.aws.aws_secret',
                          [anchor_secret_prefix, anchor_node] | join('/')) | from_json }}"
  no_log: true

- name: Render service .env
  ansible.builtin.copy:
    content: |
      {% for k, v in _anchor_secrets.items() %}
      {{ k }}={{ v }}
      {% endfor %}
    dest: /var/lib/myservice/.env
    owner: myservice
    group: myservice
    mode: '0600'
  no_log: true
```

The daemon's IAM identity needs `secretsmanager:GetSecretValue` on
`arn:aws:secretsmanager:<region>:<account>:secret:<prefix>/*`. The
`amazon.aws` Ansible collection and `boto3` must be present on the target.

See [`specs/08_secrets.md`](specs/08_secrets.md) for the full contract:
naming, JSON schema discipline, `no_log` requirement, override escape hatch,
and rotation semantics.

---

## Systemd

```bash
sudo cp systemd/anchor.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now anchor
```

---

## install.sh options

| Flag | Required | Description |
|---|---|---|
| `--bucket` | yes | S3 bucket (optionally with prefix) |
| `--node` | yes | Node path (`home/production/web_server`) |
| `--region` | yes | AWS region |
| `--dynamodb-table` | no | DynamoDB table name (default: `fleet-state`) |
| `--poll-interval` | no | Poll interval (default: `5m`) |
| `--access-key-id` | no | AWS access key |
| `--secret-access-key` | no | AWS secret key |
| `--aws-profile` | no | AWS profile name |
| `--version` | no | Anchor version to install (default: latest) |
| `--install-dir` | no | Binary install path (default: `/usr/local/bin`) |
| `--setup-systemd` | no | Install and enable systemd service |

---

## License

MIT — see [LICENSE](LICENSE).
