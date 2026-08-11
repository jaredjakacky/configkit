SHELL := /bin/sh
.ONESHELL:
.SHELLFLAGS := -eu -c

GO ?= go
GOFMT ?= gofmt

PKGS ?= ./...
COVER_PKGS ?= $(shell $(GO) list $(PKGS) | grep -v '/examples')
GOFILES := $(filter-out $(shell git ls-files --deleted -- '*.go'),$(shell git ls-files -- '*.go'))
EXAMPLE_PKGS ?= $(shell $(GO) list ./examples/...)
GOVULNCHECK_VERSION ?= v1.6.0
ALLOW_TIDY_CHANGES ?= 0

# Keep build cache inside the repo so local runs are reproducible and do not
# depend on a writable global cache path.
export GOCACHE ?= $(CURDIR)/.cache/go-build

.DEFAULT_GOAL := help

.PHONY: \
	help \
	build-examples \
	fmt \
	fmt-check \
	vet \
	test \
	test-race \
	coverage \
	root-deps-check \
	tidy \
	tidy-check \
	govulncheck \
	verify \
	release-state-check \
	release-check \
	clean

help: ## Show available targets.
	@printf "Available targets:\n"
	awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build-examples: ## Compile the runnable example programs.
	@echo "==> build examples"
	$(GO) test -run '^$$' $(EXAMPLE_PKGS)

fmt: ## Format tracked Go source files.
	@echo "==> formatting"
	$(GOFMT) -w $(GOFILES)

fmt-check: ## Verify tracked Go source files are formatted.
	@echo "==> checking formatting"
	out="$$( $(GOFMT) -l $(GOFILES) )"
	if [ -n "$$out" ]; then
		echo "The following files are not formatted:"
		echo "$$out"
		exit 1
	fi

vet: ## Run go vet on all packages.
	@echo "==> vet"
	$(GO) vet $(PKGS)

test: ## Run tests for all packages.
	@echo "==> test"
	$(GO) test $(PKGS)

test-race: ## Run tests with the race detector enabled.
	@echo "==> test race"
	$(GO) test -race $(PKGS)

coverage: ## Run library package tests with coverage output written to coverage.out.
	@echo "==> coverage"
	$(GO) test -coverprofile=coverage.out $(COVER_PKGS)
	$(GO) tool cover -func=coverage.out | tail -1

root-deps-check: ## Verify the root package compiles only Configkit, Opskit, and the standard library.
	@echo "==> checking root dependency boundary"
	raw_deps="$$( $(GO) list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' . )"
	deps="$$( printf '%s\n' "$$raw_deps" | sed '/^$$/d' | sort )"
	expected="$$( printf '%s\n' \
		'github.com/jaredjakacky/configkit' \
		'github.com/jaredjakacky/opskit' )"
	if [ "$$deps" != "$$expected" ]; then
		echo "Unexpected non-standard-library packages in the root build:"
		printf '%s\n' "$$deps"
		exit 1
	fi

tidy: ## Run go mod tidy and fail on go.mod/go.sum changes unless allowed.
	@echo "==> tidy"
	$(GO) mod tidy
	if [ "$(ALLOW_TIDY_CHANGES)" != "1" ]; then
		if ! git diff --quiet -- go.mod go.sum 2>/dev/null; then
			echo "go mod tidy changed go.mod/go.sum. Commit the changes or rerun with ALLOW_TIDY_CHANGES=1."
			set +e
			git --no-pager diff -- go.mod go.sum
			set -e
			exit 1
		fi
	fi

tidy-check: ## Verify go.mod/go.sum are already tidy.
	@$(MAKE) tidy ALLOW_TIDY_CHANGES=0

govulncheck: ## Run the pinned govulncheck tool against the main module packages.
	@echo "==> govulncheck"
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(PKGS)

verify: fmt-check root-deps-check vet test build-examples tidy-check ## Run the local verification suite.
	@echo "==> verification passed"

release-state-check: ## Verify the local checkout is the pushed main release tip.
	@echo "==> checking release state"
	if ! branch="$$(git symbolic-ref --quiet --short HEAD)"; then
		echo "Release checks require an attached main branch."
		exit 1
	fi
	if [ "$$branch" != "main" ]; then
		echo "Release checks must run from main; current branch is $$branch."
		exit 1
	fi

	state="$$(git status --porcelain=v1 --untracked-files=all --ignore-submodules=none)"
	if [ -n "$$state" ]; then
		echo "Release checks require a clean working tree:"
		printf '%s\n' "$$state"
		exit 1
	fi

	if ! git fetch --quiet --no-tags origin '+refs/heads/main:refs/remotes/origin/main'; then
		echo "Could not refresh origin/main."
		exit 1
	fi

	head="$$(git rev-parse --verify 'HEAD^{commit}')"
	remote_main="$$(git rev-parse --verify 'refs/remotes/origin/main^{commit}')"
	if [ "$$head" != "$$remote_main" ]; then
		echo "HEAD ($$head) does not match origin/main ($$remote_main)."
		exit 1
	fi

release-check: release-state-check ## Validate the exact pushed commit before tagging.
	@echo "==> validating committed release tree"
	release_commit="$$(git rev-parse --verify 'HEAD^{commit}')"
	release_root="$$(mktemp -d "$${TMPDIR:-/tmp}/configkit-release.XXXXXX")"
	release_dir="$$release_root/tree"

	cleanup() {
		git worktree remove --force "$$release_dir" >/dev/null 2>&1 || true
		if [ -n "$${release_root:-}" ]; then
			rm -rf "$$release_root"
		fi
	}
	trap cleanup 0 HUP INT TERM

	git worktree add --quiet --detach "$$release_dir" "$$release_commit"

	GOWORK=off $(MAKE) -C "$$release_dir" verify
	GOWORK=off $(MAKE) -C "$$release_dir" test-race
	GOWORK=off $(MAKE) -C "$$release_dir" govulncheck

	state="$$(git -C "$$release_dir" status --porcelain=v1 --untracked-files=all --ignore-submodules=none)"
	if [ -n "$$state" ]; then
		echo "Release validation modified the committed worktree:"
		printf '%s\n' "$$state"
		exit 1
	fi

	$(MAKE) release-state-check
	echo "==> release checks passed for $$release_commit"

clean: ## Remove local build outputs and caches.
	@echo "==> clean"
	rm -rf .cache .bin coverage.out
