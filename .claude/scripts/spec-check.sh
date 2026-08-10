#!/usr/bin/env bash
# Pre-tool hook: enforces spec-before-code discipline autonomously.
#
# Before editing any code file in this project, Claude MUST:
#   1. Read the relevant spec in specs/
#   2. Write a spec-ack file: .claude/spec-ack
#      Format: spec: <filename>  (e.g. "spec: 03_daemon_loop.md")
#   3. Then make the code edit — this hook validates and allows it.
#
# The ack is valid for 10 minutes. After that it is stale and Claude
# must re-ack (read the spec again and re-create the file).
#
# Exit codes:
#   0  — allow the tool call
#   2  — block the tool call

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$(realpath "${BASH_SOURCE[0]}")")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SPECS_DIR="$PROJECT_ROOT/specs"
ACK_FILE="$PROJECT_ROOT/.claude/spec-ack"
ACK_MAX_AGE_SECS=600

input=$(cat)
file_path=$(python3 -c "
import json, sys
d = json.load(sys.stdin)
print(d.get('file_path', d.get('path', '')))
" <<< "$input" 2>/dev/null || echo "")

[[ -z "$file_path" || "$file_path" != "$PROJECT_ROOT"* ]] && exit 0
[[ "$file_path" == "$PROJECT_ROOT/.claude"* ]] && exit 0
[[ "$file_path" == "$SPECS_DIR"* ]] && exit 0

ext="${file_path##*.}"
case "$ext" in
  go|sh|toml|yaml|yml) : ;;
  *) exit 0 ;;
esac

block() {
  cat >&2 <<EOF

╔══════════════════════════════════════════════════════════════════╗
║  SPEC-CHECK BLOCKED                                              ║
╠══════════════════════════════════════════════════════════════════╣
║  $1
╠══════════════════════════════════════════════════════════════════╣
║  TO PROCEED:                                                     ║
║  1. Read the relevant spec in specs/                             ║
║  2. Create the ack file before making the code edit:             ║
║                                                                  ║
║     printf 'spec: 03_daemon_loop.md\n' > .claude/spec-ack        ║
║                                                                  ║
║  Specs: $(ls "$SPECS_DIR"/*.md 2>/dev/null | xargs -I{} basename {} | tr '\n' ' ')
╚══════════════════════════════════════════════════════════════════╝
EOF
  exit 2
}

if [[ ! -f "$ACK_FILE" ]]; then
  block "No spec-ack found. Read the spec first.                        "
fi

ack_age=$(( $(date +%s) - $(stat -c %Y "$ACK_FILE") ))
if (( ack_age > ACK_MAX_AGE_SECS )); then
  block "spec-ack is stale (${ack_age}s old, max ${ACK_MAX_AGE_SECS}s). Re-read the spec."
fi

spec_ref=$(grep -m1 "^spec:" "$ACK_FILE" | sed 's/spec:[[:space:]]*//' | tr -d '[:space:]')
if [[ -z "$spec_ref" ]]; then
  block "spec-ack has no 'spec: <filename>' line.                       "
fi

if [[ ! -f "$SPECS_DIR/$spec_ref" ]]; then
  block "spec-ack references '$spec_ref' which does not exist in specs/"
fi

exit 0
