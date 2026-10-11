.PHONY: test check lint shellcheck actionlint local-diff-check security build-check coverage-check build package package-smoke clean live

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

# Local helper: whitespace errors in uncommitted changes. Not part of make
# check — on a fresh checkout the index matches HEAD and it can find nothing.
local-diff-check:
	@if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then git diff --check; else printf 'warning: not a git work tree; skipping git diff --check\n' >&2; fi

security:
	govulncheck ./...

build-check:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./internal/app/testdata

coverage-check:
	@set -eu; \
	profile=$$(mktemp); \
	trap 'rm -f "$$profile"' EXIT HUP INT TERM; \
	go test -race -count=1 ./... -coverprofile="$$profile"; \
	total=$$(go tool cover -func="$$profile" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}'); \
	awk -v got="$$total" -v min="$(COVERAGE_MIN)" 'BEGIN { if (got + 0 < min + 0) { printf "coverage %.1f%% is below minimum %.1f%%\n", got, min; exit 1 } printf "coverage %.1f%% (minimum %.1f%%)\n", got, min }'

check: lint shellcheck actionlint security build-check coverage-check
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
	@set -eu; \
	version=$$(cat internal/app/VERSION); \
	work=$$(mktemp -d); \
	trap 'rm -rf "$$work"' EXIT HUP INT TERM; \
	test -f "dist/abx-$$version-linux-amd64.tar.gz"; \
	test -f "dist/abx-$$version-source.tar.gz"; \
	test -f "dist/abx-$$version-SHA256SUMS"; \
	(cd dist && sha256sum -c "abx-$$version-SHA256SUMS"); \
	tar -xzf "dist/abx-$$version-linux-amd64.tar.gz" -C "$$work"; \
	test -x "$$work/abx-$$version-linux-amd64/abx"; \
	if [ "$$(id -u)" -ne 0 ]; then \
		"$$work/abx-$$version-linux-amd64/abx" version > "$$work/version.out"; \
		printf 'abx %s\n' "$$version" | cmp -s - "$$work/version.out"; \
	else \
		echo "note: built as root, so the archive was not executed: abx refuses to run as root, which is the correct behaviour, not a broken archive"; \
	fi; \
	test -f "$$work/abx-$$version-linux-amd64/LICENSE"; \
	test -f "$$work/abx-$$version-linux-amd64/THIRD_PARTY_LICENSES"; \
	test -f "$$work/abx-$$version-linux-amd64/README.md"; \
	test -f "$$work/abx-$$version-linux-amd64/README.ru.md"; \
	test -f "$$work/abx-$$version-linux-amd64/CHANGELOG.md"; \
	test -d "$$work/abx-$$version-linux-amd64/docs"; \
	test -d "$$work/abx-$$version-linux-amd64/docs/ru"; \
	! test -e "$$work/abx-$$version-linux-amd64/AGENTS.md"; \
	! test -e "$$work/abx-$$version-linux-amd64/AGENTS.ru.md"; \
	tar -xzf "dist/abx-$$version-source.tar.gz" -C "$$work"; \
	test -d "$$work/abx-$$version-source/cmd"; \
	test -d "$$work/abx-$$version-source/internal"; \
	test -d "$$work/abx-$$version-source/scripts"; \
	test -d "$$work/abx-$$version-source/docs"; \
	test -d "$$work/abx-$$version-source/.github"; \
	test -f "$$work/abx-$$version-source/go.mod"; \
	test -f "$$work/abx-$$version-source/go.sum"; \
	test -f "$$work/abx-$$version-source/Makefile"; \
	test -f "$$work/abx-$$version-source/README.md"; \
	test -f "$$work/abx-$$version-source/README.ru.md"; \
	test -f "$$work/abx-$$version-source/AGENTS.md"; \
	test -f "$$work/abx-$$version-source/AGENTS.ru.md"; \
	test -f "$$work/abx-$$version-source/CHANGELOG.md"; \
	test -f "$$work/abx-$$version-source/LICENSE"; \
	test -f "$$work/abx-$$version-source/THIRD_PARTY_LICENSES"; \
	test -f "$$work/abx-$$version-source/.gitignore"; \
	test -f "$$work/abx-$$version-source/.golangci.yml"; \
	test "$$(cat "$$work/abx-$$version-source/internal/app/VERSION")" = "$$version"; \
	(cd "$$work/abx-$$version-source" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /dev/null ./cmd/abx); \
	printf 'package smoke ok: %s\n' "$$version"

clean:
	rm -rf ./abx dist coverage.out

# Explicit opt-in on a supported non-root Linux host.
live:
	ABX_LIVE=1 go test -count=1 ./...
