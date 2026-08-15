# 00 — Overview

## What Anchor Is

Anchor is a lightweight pull-based configuration management daemon. It polls an S3
control plane for a desired-state tree, downloads it when it changes, and applies
it locally via `ansible-playbook`. No master server. No proprietary DSL. No lock-in.

Pull-based and masterless, Anchor scales horizontally without architectural changes —
each machine polls S3 independently, so fleet size is not a constraint. Target
audience: any team that needs fleet-wide convergence without a master server,
proprietary DSL, or lock-in.

**One-sentence pitch:** One Ansible codebase. A tiny daemon keeps every machine
converged. Fire the daemon and you degrade gracefully to manual applies.

---

## Taxonomy

Three layers, each building on the one below:

| Layer | Directory | Concept |
|---|---|---|
| **Role** | `roles/` | Reusable Ansible role. One technology. Generic across customers. |
| **Playbook** | `playbooks/` | Business-layer composition. Assembles roles for a specific purpose. |
| **Node** | `nodes/<node>.yml` | Machine identity. Single file: assembles playbooks and sets vars. One per machine. |

**One node per machine, always.** If a machine needs to do two things, create a
node whose playbook imports both.

Ansible's internal "role" concept maps to our **Role** layer. "Playbook" in our
taxonomy is a logical grouping, not just a YAML file — though it is implemented
as one.

---

## Key Design Decisions

**S3 as control plane.** Immutable sha trees under `commits/<sha>/`. An atomic
`current` file holds the active sha. Operators push a new tree, flip `current`.
Daemons notice on next poll.

**Pull-based.** Each machine polls independently. No SSH, no push, no master.

**Idempotency as the sole invariant.** Playbooks must be idempotent. Enforced in
CI by double-apply (`changed=0`). No phase tags. No build-time/runtime splits.

**No rollback on failure.** Apply fails → machine stays on previous state.
Rollback = operator flips `current` back to previous sha.

**Skip, don't queue.** If an apply is running when the next poll fires, skip it.
The apply loop converges; one skipped poll is harmless.

**Canary via hostname list.** Per-node `canary.txt` — one hostname per line.
Absent = everyone gets it. Present = listed hosts only. Empty = no one gets it.

**Host-specific variable overrides via `host_vars/`.** Ansible's native mechanism.
Daemon uses the real hostname in the inventory so Ansible resolves `host_vars/<hostname>/`
automatically. Variables only — no host-specific tasks.

---

## Non-Goals

- Packer / AMI baking — out of scope
- Push-based applies
- Multi-node per machine
- Rollback automation
- Ansible Galaxy / remote role fetching — tree is self-contained
- Secrets management — use AWS Secrets Manager or Parameter Store at apply time
  via Ansible lookups

---

## Project Name

**Anchor.** Binary: `anchor`. Config: `/etc/anchor/anchor.toml`.
Working directory: `/var/lib/anchor/`. Systemd unit: `anchor.service`.
