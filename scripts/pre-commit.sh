#!/usr/bin/env bash
# Installed into .git/hooks/pre-commit by `make hooks-install`.
# Keeps the repo from accumulating unformatted/unvetted/failing Go code.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "[pre-commit] gofmt"
unformatted=$(gofmt -l $(git diff --cached --name-only --diff-filter=ACM -- '*.go'))
if [ -n "$unformatted" ]; then
  echo "The following files are not gofmt'd:"
  echo "$unformatted"
  echo "Run 'make fmt' and re-stage."
  exit 1
fi

echo "[pre-commit] go vet"
go vet ./...

echo "[pre-commit] go test"
go test ./... -race

echo "[pre-commit] all checks passed"
