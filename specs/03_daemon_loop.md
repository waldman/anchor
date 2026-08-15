# 03 — Daemon Loop

## State Machine

```
┌─────────┐
│  Idle   │◄──────────────────────────────────────────┐
└────┬────┘                                           │
     │ poll_interval elapsed                          │
     ▼                                                │
┌─────────┐   sha unchanged                           │
│ Polling │──────────────────────────────────────────►│
└────┬────┘                                           │
     │ sha changed                                    │
     ▼                                                │
┌──────────┐  canary: skip                            │
│ Fetching │─────────────────────────────────────────►│
└────┬─────┘                                          │
     │ tree downloaded                                │
     ▼                                                │
┌──────────┐  success                                 │
│ Applying │─────────────────────────────────────────►│
└────┬─────┘                                          │
     │ failure                                        │
     ▼                                                │
┌───────────┐                                         │
│  Failed   │─────────────────────────────────────────┘
└───────────┘
```

## Concurrency Rule

If an apply is in progress when the next poll fires: **skip the poll, log it,
resume on the following interval.** No queue, no cancellation, no parallel runs.
At most one apply runs at any time.

## Poll Cycle Detail

1. **Read `current`** from S3: `GET <prefix>/current`
2. **Compare** to `last_sha` (in-memory). If equal → back to Idle.
3. **Check canary**: `GET <prefix>/commits/<sha>/canary/<node>.txt`
   - Absent → proceed
   - Present → check hostname; skip if not listed
4. **Download tree**: copy `commits/<sha>/` to `working_dir/staging/<sha>/`
5. **Verify**: staging directory must contain `nodes/<node>.yml`
6. **Write inventory**: create `<staging>/inventory` with the local hostname
   (single line). Needed for Ansible's `host_vars/` discovery.
7. **Atomic swap**: rename `staging/<sha>/` to `working_dir/current/`
8. **Apply**: run `ansible-playbook` (see `05_runner.md`)
9. **Write state**: update local `state.json` and DynamoDB (see `06_state.md`)
10. **Update `last_sha`**: set to new sha on success; leave unchanged on failure

## Working Directory Layout

```
/var/lib/anchor/
  current/                  # symlink or directory — the active sha tree
  staging/
    <sha>/                  # in-progress download; deleted on failure
  state.json                # local state (see 06_state.md)
```

Staging is always cleaned up: on success (renamed to `current/`), on failure
(deleted). The daemon never leaves partial trees behind.

## Startup Behaviour

On first start with an empty working directory:
1. Poll `current` → get sha
2. Check canary
3. Download tree → apply
4. Write state

No special "first boot" mode. The loop is identical from the first iteration.

## Shutdown

On `SIGTERM` or `SIGINT`:
- If idle: exit immediately
- If applying: let the current apply finish, then exit

No in-flight apply is killed. Ansible mid-run is not safe to interrupt.

## Error Handling

| Error | Action |
|---|---|
| S3 `current` unreachable | Log, back to Idle, retry next interval |
| Canary file S3 error | Log, treat as "skip" (safe default) |
| Download failure | Log, delete staging, back to Idle |
| `ansible-playbook` non-zero exit | Log, write failed state, back to Idle on previous sha |
| DynamoDB write failure | Log, continue — state write is best-effort |

State write failure is non-fatal. The apply already happened; losing the DynamoDB
record is an observability loss, not an operational failure.
