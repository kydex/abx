.PHONY: test check lint shellcheck actionlint diff-check security build-check coverage-check build package live

COVERAGE_MIN ?= 74.0

test:
	go test -count=1 ./...

lint:
	golangci-lint fmt --diff ./...
	golangci-lint run ./...

shellcheck:
	shellcheck scripts/*.sh

actionlint:
	actionlint .github/workflows/*.yml

diff-check:
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git diff --check; else printf 'warning: not a git work tree; skipping git diff --check\n' >&2; fi

security:
	govulncheck ./...

build-check:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx

coverage-check:
	@set -eu; \
	profile=$$(mktemp); \
	trap 'rm -f "$$profile"' EXIT HUP INT TERM; \
	go test -race -count=1 ./... -coverprofile="$$profile"; \
	total=$$(go tool cover -func="$$profile" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v got="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { if (got + 0 < min + 0) { printf "coverage %.1f%% is below minimum %.1f%%\n", got, min; exit 1 } printf "coverage %.1f%% (minimum %.1f%%)\n", got, min }'

check: lint shellcheck actionlint diff-check security build-check coverage-check

build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o abx ./cmd/abx

package:
	@version=$$(cat internal/app/VERSION); \
	test -n "$$version" || { echo "error: cannot read version from internal/app/VERSION" >&2; exit 1; }; \
	bash scripts/package.sh "v$$version"

# Explicit opt-in on a supported non-root Linux host.
live:
	ABX_LIVE=1 go test -count=1 ./...
