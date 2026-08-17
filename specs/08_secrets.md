# 08 — Secrets

## Scope

Anchor does not fetch, cache, or materialize secrets. Secret retrieval is
Ansible's responsibility, using its native lookup plugins against AWS Secrets
Manager. This spec documents what the daemon contributes to that flow (a
configurable prefix and the extra-vars it injects into `ansible-playbook`)
and the convention consumers should follow when storing and consuming
secrets.

Non-goals:

- No secret fetching in the Go daemon.
- No prefetch, cache, or on-disk secrets tree.
- No first-class "secret" object in anchor's data model.

The runner's existing contract (spec 05) — Ansible inherits the daemon's AWS
credentials via the ambient credential chain — is what makes this work.

## Daemon Contribution

Two extra-vars are passed to every `ansible-playbook` invocation:

| Var | When | Source |
|---|---|---|
| `anchor_node` | always | `[daemon].node` from anchor.toml |
| `anchor_secret_prefix` | only when `[secrets].prefix` is set | `[secrets].prefix` from anchor.toml |

Both are Ansible extra-vars (`-e key=value`), not environment variables. See
spec 05 for the invocation.

The prefix is a passthrough — anchor does not validate its format, does not
call AWS with it, and does not verify that the derived secret exists.

## Recommended Convention

### One secret per node

The AWS Secrets Manager secret name is derived from node identity:

```
<prefix>/<anchor_node>
```

For `[secrets].prefix = "anchor"` and node `home/production/hermes-ada`, that
is:

```
anchor/home/production/hermes-ada
```

**One secret per node.** No sub-paths, no per-role secrets, no per-key
secrets. The path mirrors the `nodes/` tree layout 1:1.

### Secret value: flat JSON, string values, UPPERCASE keys

The secret's value is a JSON object:

- Top-level object, no nesting.
- All values are strings.
- Keys are `UPPER_SNAKE_CASE` so they template directly into `.env`-style
  files without transformation.

Example:

```json
{
  "OPENROUTER_API_KEY": "sk-or-...",
  "WHATSAPP_ENABLED": "true",
  "WHATSAPP_ALLOWED_USERS": "+5511981125205"
}
```

Multi-line values (PEM bundles, service-account JSON) are stored as a single
string with `\n` escaped inside the JSON. Consumers write them verbatim.

If a role thinks it needs nested objects or a second secret bundle, that is
the signal to split the node — not to complicate the schema.

## Ansible Recipe

Load the bundle once per apply as a role-local fact:

```yaml
- name: Load anchor secret bundle
  ansible.builtin.set_fact:
    _anchor_secrets: "{{ lookup('amazon.aws.aws_secret',
                          [anchor_secret_prefix, anchor_node] | join('/')) | from_json }}"
  no_log: true
```

Consume by key:

```yaml
- name: Render service .env
  ansible.builtin.copy:
    content: |
      {% for k, v in _anchor_secrets.items() %}
      {{ k }}={{ v }}
      {% endfor %}
    dest: /var/lib/hermes/.env
    owner: hermes
    group: hermes
    mode: '0600'
  no_log: true
```

### `no_log` discipline

Every task whose input or output touches a secret value uses `no_log: true`.
Ansible's default logging would otherwise emit the secret in `-v` output and
in `changed=` diffs. There is no partial-logging escape: `no_log` is
mandatory on both the fetch and every consumer task.

### Missing keys fail loudly

Consumers reference keys directly (`_anchor_secrets['OPENROUTER_API_KEY']`
or iteration). A missing key raises a Jinja `KeyError` at template time and
the apply fails. This is intentional: silent defaults for absent secrets
hide bootstrap mistakes.

## Escape Hatch (Discouraged)

A role that legitimately needs a different secret bundle — a secret shared
across nodes, a third-party integration credential — sets its own name:

```yaml
# roles/<role>/defaults/main.yml
_anchor_secret_bundle_name: "{{ [anchor_secret_prefix, anchor_node] | join('/') }}"
```

Node file can override:

```yaml
# nodes/<node>.yml
vars:
  _anchor_secret_bundle_name: "anchor/shared/openrouter"
```

Role uses the variable in place of the inline derivation. Reserve this for
cases that cannot be modelled as "the node owns its own secrets."

## IAM

The daemon's IAM identity (see spec 07) needs
`secretsmanager:GetSecretValue` on the prefix:

```
arn:aws:secretsmanager:<region>:<account>:secret:<prefix>/*
```

Anchor does not create, rotate, or manage secrets. Provisioning the SM
secret is the operator's responsibility, once per node:

```bash
aws secretsmanager create-secret \
  --name anchor/home/production/hermes-ada \
  --secret-string '{"OPENROUTER_API_KEY":"..."}'
```

## Rotation

Every apply fetches the current secret value. Rotation is "edit the SM
secret; next apply picks it up." No cache, no invalidation, no signal to the
daemon.

## Prerequisites on the Target

The `amazon.aws` Ansible collection must be installed on the target. This
is a consumer-tree concern (list it in `requirements.yml` and install via
`ansible-galaxy collection install -r requirements.yml`), not anchor's
concern.

`boto3` is a transitive Python dependency of the collection and must be
importable by the Python interpreter Ansible uses.

## Related Specs

- Spec 02 — `[secrets].prefix` field
- Spec 05 — runner extra-vars invocation
- Spec 07 — daemon IAM identity
