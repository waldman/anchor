# 04 — Canary

## Purpose

Canary allows a new sha to be rolled out to a subset of machines before the full
fleet applies it. The operator controls which machines are "canary" by listing
their hostnames in `canary.txt`.

## File Location

```
commits/<sha>/canary/<location>/<environment>/<name>.txt
```

The file lives at the top level of the sha tree, keyed by node path. Different
nodes in the same sha have independent canary lists.

Canary is intentionally outside `nodes/` because it is deployment-time metadata
about a specific SHA rollout, not part of the node's identity. See `01_s3_layout.md`.

## Semantics

| State | Behaviour |
|---|---|
| File absent (S3 404) | All machines apply |
| File present, hostname listed | This machine applies |
| File present, hostname not listed | This machine skips |
| File present, empty | No machine applies (emergency block) |

## File Format

```
# Lines starting with '#' are comments — ignored
# Blank lines are ignored

web-01.example.com
web-02.example.com
```

- One fully-qualified hostname per line
- **Exact match only** — `web-01` does not match `web-01.example.com`
- Hostname compared against `os.Hostname()` at runtime
- Leading and trailing whitespace stripped per line

## Daemon Behaviour

The canary check happens **after** sha comparison but **before** download:

1. sha changed → check canary
2. Canary: skip → log reason, write `skipped` state to DynamoDB, back to Idle
3. Canary: apply → proceed to download

A skipped apply is recorded in DynamoDB with `status: skipped` and the candidate
sha. This lets operators see which machines are holding back from a canary sha.

## S3 Error Handling

If the canary file cannot be fetched due to an S3 error (not a 404 — an actual
error), the daemon treats it as **skip** (safe default). Log the error. Do not
apply. Retry on next poll.

## Typical Canary Rollout Flow

1. Push new tree to `commits/<sha>/`
2. Create `canary.txt` listing one or two hostnames
3. Flip `current` to new sha
4. Monitor DynamoDB: canary machines apply, rest show `skipped`
5. Validate canary machines are healthy
6. Delete or empty `canary.txt` — on next poll, full fleet applies

To abort: flip `current` back to previous sha.
