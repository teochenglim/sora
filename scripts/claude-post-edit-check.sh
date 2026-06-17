#!/usr/bin/env bash
# Claude Code PostToolUse hook (see .claude/settings.json).
# Runs after Claude edits/writes a .go file: gofmt + go vet, fast feedback
# loop so the agent sees and fixes issues immediately instead of at the
# end of a long session.
set -uo pipefail

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

input=$(cat)
file_path=$(echo "$input" | grep -o '"file_path"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed -E 's/.*"file_path"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/')

case "$file_path" in
  *.go)
    ;;
  *)
    exit 0
    ;;
esac

if [ ! -f "$file_path" ]; then
  exit 0
fi

unformatted=$(gofmt -l "$file_path")
if [ -n "$unformatted" ]; then
  echo "gofmt: $file_path is not formatted; run 'gofmt -w $file_path'" >&2
  exit 2
fi

vet_dir=$(dirname "$file_path")
if ! go vet "$vet_dir" >/tmp/sora-vet.log 2>&1; then
  echo "go vet failed for $file_path:" >&2
  cat /tmp/sora-vet.log >&2
  exit 2
fi

exit 0
