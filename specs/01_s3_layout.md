# 01 — S3 Layout

## Tree Structure

```
s3://<bucket>/
  current                              # active sha — the only file operators update to deploy
  commits/
    <sha>/
      ansible.cfg                      # roles_path = roles:playbooks
      roles/
        <role-name>/
          tasks/
            main.yml
          handlers/
          templates/
          defaults/
          vars/
      playbooks/
        <playbook-name>/
          tasks/
            main.yml
          vars/
            main.yml
      nodes/
        <location>/
          <environment>/
            <name>/
              playbook.yml             # imports playbooks/
              canary.txt               # optional — controls who gets this sha
              host_vars/
                <hostname>/
                  main.yml             # variable overrides — no tasks
```

## `ansible.cfg`

Every sha tree contains an `ansible.cfg` at the tree root:

```ini
[defaults]
roles_path = roles:playbooks
host_key_checking = False
```

`roles_path = roles:playbooks` allows playbooks to reference both Ansible roles
and Anchor playbooks by name interchangeably. Anchor playbooks reference roles;
node playbooks reference Anchor playbooks. Ansible resolves both.

## `current` Pointer

A plain text file containing a single sha string (no newline required, trimmed on
read). The daemon polls this file and compares it against the last-applied sha.

Operator deploy flow:
1. Push new tree to `commits/<sha>/`
2. Write sha to `current`

The two steps are intentionally separate — the tree must exist before `current`
is flipped.

## Key Prefixes

The `bucket` config field can encode a key prefix by including a `/`:

```
"my-fleet-config"            → bucket=my-fleet-config, prefix=""
"my-fleet-config/production" → bucket=my-fleet-config, prefix=production/
```

S3 bucket names cannot contain `/` — the daemon splits on the first `/` and
treats the remainder as a key prefix. All keys are prefixed accordingly:
`production/current`, `production/commits/<sha>/...`.

## Node Path

A node is identified by its path within `nodes/`: `<location>/<environment>/<name>`.

Examples:
- `home/production/acme_webserver`
- `us-east/staging/payments_redis`

The daemon config field `node` holds this path. The full S3 key for the node's
entry point is:

```
commits/<sha>/nodes/<node>/playbook.yml
```

## `canary.txt`

Located at `commits/<sha>/nodes/<location>/<environment>/<name>/canary.txt`.

Semantics:
- **Absent (404)** → all hosts apply
- **Present, hostname listed** → this host applies
- **Present, hostname not listed** → this host skips
- **Present, empty** → no host applies (emergency block)

One hostname per line. Exact match — `web-01` does not match `web-01.example.com`.
Lines starting with `#` and blank lines are ignored.

## `host_vars/`

Located at `commits/<sha>/nodes/<node>/host_vars/<hostname>/main.yml`.

Variables only. No tasks, no handlers, no role includes. These override variables
set in the node's playbook and the playbooks it imports.

Ansible resolves `host_vars/` automatically when the daemon uses the real hostname
in the inventory (see `05_runner.md`).

## Self-Contained Trees

Each sha tree is a complete, self-contained snapshot. Roles are duplicated across
trees — that redundancy is intentional. S3 deduplication handles storage; the
guarantee is that any sha can be reproduced exactly without external dependencies.
