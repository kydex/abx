.PHONY: test check lint shellcheck actionlint local-diff-check security build-check licenses-check coverage-check build package package-smoke package-check clean live

COVERAGE_MIN ?= 74.0

test:
	go test -count=1 ./...

lint:
	golangci-lint fmt --diff ./...
	golangci-lint run ./...
	# testdata is invisible to go list, so the two lines above never see the
	# live fixture: lint it by explicit path instead.
	golangci-lint run ./internal/app/testdata/probe.go

shellcheck:
	shellcheck scripts/*.sh

actionlint:
	actionlint .github/workflows/*.yml

# Local helper: whitespace errors in uncommitted changes. Not part of make
# check — on a fresh checkout the index matches HEAD and it can find nothing.
local-diff-check:
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git diff --check; else printf 'warning: not a git work tree; skipping git diff --check\n' >&2; fi

security:
	govulncheck ./...

build-check:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./internal/app/testdata

# Both archives carry THIRD_PARTY_LICENSES, so a module used by the build must
# appear there; the rows are counted against go.mod's module list.
licenses-check:
	@set -eu; \
	want=$$(($$(go list -m all | wc -l) - 1)); \
	got=$$(sed -n '/^- /p' THIRD_PARTY_LICENSES | wc -l); \
	[ "$$got" -eq "$$want" ] || { echo "error: THIRD_PARTY_LICENSES lists $$got modules, the build uses $$want" >&2; exit 1; }; \
	echo "THIRD_PARTY_LICENSES matches the build's module list"

coverage-check:
	@set -eu; \
	profile=$$(mktemp); \
	trap 'rm -f "$$profile"' EXIT HUP INT TERM; \
	go test -race -count=1 ./... -coverprofile="$$profile"; \
	total=$$(go tool cover -func="$$profile" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v got="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { if (got + 0 < min + 0) { printf "coverage %.1f%% is below minimum %.1f%%\n", got, min; exit 1 } printf "coverage %.1f%% (minimum %.1f%%)\n", got, min }'

check: lint shellcheck actionlint security build-check licenses-check package-check coverage-check
	go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o abx ./cmd/abx

package:
	@version=$$(cat internal/app/VERSION); \
	test -n "$$version" || { echo "error: cannot read version from internal/app/VERSION" >&2; exit 1; }; \
	bash scripts/package.sh "v$$version"

# Verifies the checksum manifest, unpacks both archives and checks structure
# against the mandatory file list plus the embedded version.
# The release binary itself is not executed, so this works for any caller.
package-smoke:
	@bash scripts/package-smoke.sh dist

# Rehearses release packaging on any branch: builds both archives into a
# throwaway directory without a release tag or cleanliness gate and runs the
# same smoke checks, so a packaging regression fails here instead of only at
# the tag.
package-check:
	@set -eu; \
	work=$$(mktemp -d); \
	trap 'rm -rf "$$work"' EXIT HUP INT TERM; \
	bash scripts/package.sh --check "$$work"; \
	bash scripts/package-smoke.sh "$$work"

clean:
	rm -rf ./abx dist coverage.out

# Explicit opt-in on a supported non-root Linux host.
live:
	ABX_LIVE=1 go test -count=1 ./...
