# 05 — Runner

## Responsibility

The runner executes `ansible-playbook` as a subprocess within the downloaded sha
tree, captures output, and returns success or failure.

## Invocation

```bash
ansible-playbook \
  -i inventory \
  -e ansible_connection=local \
  -e anchor_node=<node> \
  [-e anchor_secret_prefix=<prefix>] \
  nodes/<node>.yml
```

Working directory: `working_dir/current/` (the root of the active sha tree).
`ansible.cfg` at the tree root is picked up automatically by Ansible.

## Injected Extra-Vars

| Var | Present when | Value |
|---|---|---|
| `ansible_connection` | always | `local` |
| `anchor_node` | always | `[daemon].node` from anchor.toml — the node identity |
| `anchor_secret_prefix` | `[secrets].prefix` is set | Verbatim from `[secrets].prefix` |

`anchor_node` is exposed to Ansible so playbooks and roles can self-reference
the machine's node identity (naming, tagging, deriving secret paths). See
spec 08 for the secrets convention that consumes both variables.

The runner does not introspect or transform these values. Adding new
extra-vars is a spec change, not a runtime option.

## Inventory File

The daemon writes an `inventory` file at the tree root at fetch time — a single
line containing `os.Hostname()`. This is deliberate: an inventory *file* (rather
than inline `-i <host>,`) makes Ansible resolve `host_vars/<hostname>/` relative
to `inventory_dir`, which is the tree root — so top-level `host_vars/` gets picked
up automatically.

The hostname must be the machine's real hostname (not `localhost`) so it matches
the `host_vars/<hostname>/` directory in the tree and the canary hostname list.

## `ansible.cfg` in the Tree

```ini
[defaults]
roles_path = roles:playbooks
host_key_checking = False
```

The runner does not set `ANSIBLE_ROLES_PATH` or other env vars — the tree's
`ansible.cfg` is the authoritative configuration.

## Output Handling

- stdout and stderr are both captured and logged as structured fields at `info`
  level during the run (streamed, not buffered)
- On failure: full stdout + stderr included in the log at `error` level
- On success: summary line logged at `info` level

The runner does not parse Ansible's output beyond reading the exit code.

## Exit Code Interpretation

| Exit code | Meaning | Anchor action |
|---|---|---|
| `0` | Success | Record `success`, update sha |
| `1` | Error | Record `failed`, keep previous sha |
| `2` | Error (unreachable hosts) | Record `failed`, keep previous sha |
| `4` | Error (task failed) | Record `failed`, keep previous sha |
| `8` | Error (unreachable + failed) | Record `failed`, keep previous sha |

Any non-zero exit code is treated as failure.

## Timeout

No apply timeout in v1. Ansible manages its own timeouts via `ansible.cfg`
(`timeout`, `command_timeout`). Anchor does not kill a running apply.

## Environment

The runner inherits the daemon process environment. No additional environment
variables are injected beyond what the daemon already has. AWS credentials (for
Ansible lookups against Secrets Manager / Parameter Store) are available via the
same credential chain the daemon uses.

Secret retrieval itself happens inside Ansible via the `amazon.aws.aws_secret`
lookup plugin — see spec 08 for the convention.
