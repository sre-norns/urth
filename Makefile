# Makefile is handcrafted to automate repetitive tasks

website-dist = website/dist

# Postgres connection used by the local development targets below.
# Note: the api-server's own default is `sqlite:test.sqlite`, which is not
# supported (TIMESTAMPTZ columns cannot be read back), so Postgres is passed here.
store-url ?= postgres://urth:urth@localhost:5432/urth

# Local identity: the issuer is the browser-facing origin -- website's
# dev server, which proxies the API -- because emailed links are built from it
# and sign-in forms are only accepted from it. Mail is written as .eml files to
# .dev/mail. The bootstrap user owns an account, so it can sign straight in.
# Variables already set in the environment win. A test checks each of these is
# a flag's environment variable (pkg/apiserver/identity_test.go).
.PHONY: run-api-server
run-api-server: export URTH_DEVELOPMENT ?= true
run-api-server: export URTH_ISSUER ?= http://localhost:3001
run-api-server: export URTH_WEB_REDIRECT_URI ?= http://localhost:3001/oauth/callback
run-api-server: export URTH_MAIL_PROVIDER ?= development
run-api-server: export URTH_AUTH_MAIL_DIR ?= $(CURDIR)/.dev/mail
run-api-server: export URTH_BOOTSTRAP_EMAIL ?= admin@urth.example
run-api-server: export URTH_BOOTSTRAP_PASSWORD ?= urth-dev-password
run-api-server: # Start API server (needs Postgres and NATS)
	@go run ./cmd/api-server --store.url="$(store-url)" --nats.allow-insecure-workers --nats.allow-insecure

# The same, with sign-in through the fake identity provider as a generic OIDC
# provider. Start it first with `make run-fake-idp`.
.PHONY: run-api-server-fake-idp
run-api-server-fake-idp: export URTH_OIDC_ISSUER_URL ?= http://127.0.0.1:18090/google
run-api-server-fake-idp: export URTH_OIDC_CLIENT_ID ?= fake-client
run-api-server-fake-idp: export URTH_OIDC_CLIENT_SECRET ?= fake-secret
run-api-server-fake-idp: run-api-server

# A local fake Google/GitHub/OIDC provider on 127.0.0.1:18090. Never contacts a
# real provider; development and browser tests only.
.PHONY: run-fake-idp
run-fake-idp: # Start the fake identity provider
	@go run github.com/sre-norns/wyrd/identity/cmd/fake-idp

# Kept as an alias: NATS used to be opt-in, and docs and habits still name it.
.PHONY: run-api-server-nats
run-api-server-nats: run-api-server

# The enrolment token comes from the environment or a file rather than a flag:
# an argument is visible in the process table to every user on the host.
#   export RUNNER_TOKEN=$$(go run ./cmd/urthctl runners token -f ./examples/runner.yaml)
.PHONY: run-nats-worker
run-nats-worker: # Start NATS based worker
	@go run ./cmd/nats-worker --allow-insecure-api --nats.allow-insecure --client.token="$(RUNNER_TOKEN)"

.PHONY: run-scheduler
run-scheduler: # Start scheduler server
	@echo Not implemented yet....

website/node_modules: website/package.json website/package-lock.json
	@cd website && npm ci

.PHONY: serve-site build-site test-site
serve-site: website/node_modules
	@cd website && npm run dev

build-site: website/node_modules
	@cd website && npm run build

test-site: website/node_modules
	@cd website && npm test

.PHONY: run-postgres-podman
run-postgres-podman: # Start postgres using podman container
	@podman run -p 5432:5432 -e POSTGRES_USER=urth -e POSTGRES_PASSWORD=urth -e POSTGRES_DB=urth postgres:18

# Single non-replicated server with JetStream: fine for development, explicitly
# not highly available. Production wants three replicas on persistent volumes.
.PHONY: run-nats-podman
run-nats-podman: # Start NATS with JetStream using podman container
	@podman run -p 4222:4222 -p 8222:8222 nats:2.15-alpine -js -m 8222


# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

.PHONY: no-dirty
no-dirty:
	git diff --exit-code


# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy -v

## verify: Verify go modules and run go vet on the project
.PHONY: verify
verify:
	go mod verify
	go vet ./...

# Tool versions are pinned rather than tracked at @latest, so that a CI run
# cannot start failing on a check that no commit in this repository introduced.
# Bump these deliberately.
staticcheck-version = 2026.2.1
govulncheck-version = v1.8.0

## staticcheck: Run go static-check tool on the code-base
.PHONY: staticcheck
staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@$(staticcheck-version) -checks=all,-ST1000,-U1000 ./...

## scan-vuln: Scan for known GO-vulnarabilities
.PHONY: scan-vuln
scan-vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(govulncheck-version) ./...

## audit: run quality control checks
.PHONY: audit
audit: verify staticcheck test # scan-vuln

# What CI runs. The only difference from `audit` is that the tests requiring a
# real database actually run instead of skipping themselves -- see test/postgres.
# It is a separate target rather than a flag on `audit` so that the command CI
# runs is a command a developer can run verbatim, and so `make audit` stays
# usable with no containers.
## audit/postgres: run quality control checks including the Postgres-backed tests
.PHONY: audit/postgres
audit/postgres: verify staticcheck test/postgres


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	@go test -race -buildvcs ./...

# The dispatch outbox's guarantees -- Result/dispatch atomicity and relay row
# leasing -- are Postgres guarantees, so those tests skip themselves unless
# URTH_TEST_POSTGRES_URL is set. It is not set by default because the rest of the
# suite is deliberately runnable with no containers; CI supplies it as a service
# and runs this target through `audit/postgres`.
#
# A URL that is set but unreachable fails rather than skips, so a CI database
# that quietly went missing shows up as a failure and not as a green run.
# Run `make run-postgres-podman` first.
## test/postgres: run all tests including the Postgres-backed outbox tests
.PHONY: test/postgres
test/postgres:
	@URTH_TEST_POSTGRES_URL="$(store-url)" go test -race -buildvcs ./...

## test/verbose: run all tests with per-test output
.PHONY: test/verbose
test/verbose:
	@go test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	@go test -v -race -buildvcs -coverprofile=/tmp/coverage.out ./...
	@go tool cover -html=/tmp/coverage.out

## clean: remove build artifacts
.PHONY: clean
clean:
	$(RM) ./api-server ./nats-worker ./urthctl
	$(RM) -dr ./dist $(website-dist)


.PHONY: build api-server nats-worker urthctl
api-server:
	go build ./cmd/api-server

nats-worker:
	go build ./cmd/nats-worker

urthctl:
	go build ./cmd/urthctl

$(website-dist): build-site

build: api-server build-site nats-worker urthctl

## release/snapshot: build release archives locally without publication
.PHONY: release/snapshot
release/snapshot: build-site
	python3 scripts/build-release.py --snapshot
