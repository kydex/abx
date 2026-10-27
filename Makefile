.PHONY: test check lint shellcheck actionlint python-check links-check local-diff-check security build-check licenses-check coverage-check build package package-smoke package-check clean live

COVERAGE_MIN ?= 74.0
# The smoke script requires an explicit flag to skip execution on a host that
# is not x86_64. Pass it here so make check still checks the ELF header there.
SMOKE_FLAGS := $(if $(filter x86_64,$(shell uname -m)),,--skip-exec)

test:
	go test -count=1 ./...

lint:
	golangci-lint fmt --diff ./...
	golangci-lint run ./...
	# testdata is invisible to go list, so the commands above never see the
	# live fixture: lint and format-check it by explicit path instead.
	golangci-lint run ./internal/app/testdata/probe.go
	golangci-lint fmt --diff ./internal/app/testdata/probe.go

shellcheck:
	shellcheck scripts/*.sh

actionlint:
	actionlint .github/workflows/*.yml

# The Python tools below are not covered by any Go linter; compile both without
# writing bytecode into the source tree. compile() catches top-level returns
# that ast.parse accepts.
python-check:
	python3 -c 'import pathlib; [compile(p.read_text(encoding="utf-8"), str(p), "exec") for p in pathlib.Path("scripts").glob("*.py")]'

# Language links must resolve in packaged artifacts, where the docs tree is
# copied as-is. Only file targets are checked; heading anchors stay manual.
links-check:
	python3 scripts/check-links.py

# Local helper: whitespace errors in uncommitted changes. Not part of make
# check — on a fresh checkout the index matches HEAD and it can find nothing.
# Comparing with HEAD includes both staged and unstaged changes.
local-diff-check:
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git diff --check HEAD; else printf 'warning: not a git work tree; skipping git diff --check\n' >&2; fi

security:
	govulncheck ./...

build-check:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./internal/app/testdata

# Both archives carry THIRD_PARTY_LICENSES, so every module used by the build
# must appear there with its exact version; the list is compared with go.mod's
# module list entry by entry, not just counted.
licenses-check:
	@set -eu; \
	dir=$$(mktemp -d); \
	trap 'rm -rf "$$dir"' EXIT HUP INT TERM; \
	go list -m all | tail -n +2 | LC_ALL=C sort > "$$dir/build"; \
	sed -n 's/^- //p' THIRD_PARTY_LICENSES | awk -F ' - ' '{print $$1}' | LC_ALL=C sort > "$$dir/manifest"; \
	if ! cmp -s "$$dir/build" "$$dir/manifest"; then \
		echo "error: THIRD_PARTY_LICENSES does not match the build's module list" >&2; \
		diff -u "$$dir/manifest" "$$dir/build" >&2 || true; \
		exit 1; \
	fi; \
	echo "THIRD_PARTY_LICENSES matches the build's module list"

# Instrument every package in every test binary: a per-package profile reports
# code exercised by another package's tests as a blind spot.
coverage-check:
	@set -eu; \
	profile=$$(mktemp); \
	trap 'rm -f "$$profile"' EXIT HUP INT TERM; \
	go test -race -count=1 ./... -coverpkg=./... -coverprofile="$$profile"; \
	total=$$(go tool cover -func="$$profile" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v got="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { if (got + 0 < min + 0) { printf "coverage %.1f%% is below minimum %.1f%%\n", got, min; exit 1 } printf "coverage %.1f%% (minimum %.1f%%)\n", got, min }'

check: lint shellcheck actionlint python-check links-check security build-check licenses-check package-check coverage-check
	go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o abx ./cmd/abx

package:
	@version=$$(cat internal/app/VERSION); \
	test -n "$$version" || { echo "error: cannot read version from internal/app/VERSION" >&2; exit 1; }; \
	bash scripts/package.sh "v$$version"

# Verifies the checksum manifest, unpacks both archives, checks structure
# against the mandatory file list plus the embedded version, and checks the
# binary's ELF class and architecture.
# A non-root x86_64 caller also runs `abx version`; a root caller skips that,
# since abx refuses to run as root, and a non-root caller on another host
# architecture skips only through SMOKE_FLAGS.
package-smoke:
	@bash scripts/package-smoke.sh $(SMOKE_FLAGS) dist

# Rehearses release packaging on any branch: builds both archives into a
# throwaway directory without a release tag or cleanliness gate and runs the
# same smoke checks, so a packaging regression fails here instead of only at
# the tag.
package-check:
	@set -eu; \
	work=$$(mktemp -d); \
	trap 'rm -rf "$$work"' EXIT HUP INT TERM; \
	bash scripts/package.sh --check "$$work"; \
	bash scripts/package-smoke.sh $(SMOKE_FLAGS) "$$work"

clean:
	rm -rf ./abx dist coverage.out

# Explicit opt-in on a supported non-root Linux host.
live:
	ABX_LIVE=1 go test -count=1 ./...
