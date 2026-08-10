# 06 — State

## Two State Stores

| Store | Purpose | Audience |
|---|---|---|
| Local `state.json` | Per-machine current state | `systemctl status`, local ops |
| DynamoDB | Fleet-wide current state | Operators, dashboards |

## Local State File

**Location:** `working_dir/state.json`

```json
{
  "hostname": "web-01.example.com",
  "node": "home/production/acme_webserver",
  "sha": "abc1234def5678",
  "status": "success",
  "apply_time": "2026-08-10T14:23:01Z",
  "duration_s": 47,
  "daemon_version": "1.2.3",
  "error": ""
}
```

Written after every apply event (success, failed, skipped). Overwritten in place.
Human-readable via `cat /var/lib/anchor/state.json`.

`error` is empty string on success/skipped. Contains the error message on failure.

## DynamoDB Table

**Table name:** from `state.dynamodb_table` in config.

**Partition key:** `hostname` (String). No sort key.

One record per machine. `PutItem` on every apply — overwrites the previous record.
Table size equals fleet size, permanently.

### Record Schema

| Attribute | Type | Notes |
|---|---|---|
| `hostname` | String | PK — `web-01.example.com` |
| `node` | String | `home/production/acme_webserver` |
| `sha` | String | sha that was applied (or candidate sha for `skipped`) |
| `status` | String | `success` / `failed` / `skipped` |
| `apply_time` | String | ISO8601 UTC |
| `duration_s` | Number | 0 for `skipped` |
| `daemon_version` | String | semver |
| `error` | String | empty unless `status=failed` |
| `ttl` | Number | Unix timestamp — `apply_time + ttl_days * 86400` |

### TTL

DynamoDB TTL attribute: `ttl`. Set to `apply_time + ttl_days * 86400` (seconds).
Default: 15 days. Configurable via `state.ttl_days` in config.

If a machine is decommissioned and stops writing, its record auto-purges after
`ttl_days` days with no daemon action required.

### Write Behaviour

- **Success:** write `status=success`, `sha=<new sha>`, `duration_s=<n>`
- **Failed:** write `status=failed`, `sha=<candidate sha>`, `error=<message>`
- **Skipped:** write `status=skipped`, `sha=<candidate sha>`, `duration_s=0`

DynamoDB write failure is **non-fatal**. Log the error and continue. The apply
already completed; losing the fleet record is an observability loss, not an
operational failure.

## Fleet-Wide Queries

All queries are simple Scan operations — adequate for any fleet size where
operators are managing records per-machine interactively.

**Convergence check (all machines on current sha):**
```
Scan → filter sha == <expected sha>
```

**Failed machines:**
```
Scan → filter status == "failed"
```

**Stale machines (not seen recently):**
```
Scan → filter apply_time < <threshold>
```
