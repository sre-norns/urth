# Makefile is handcrafted to automate repetitive tasks

website-dist = website/dist

# Postgres connection used by the local development targets below.
# Note: the api-server's own default is `sqlite:test.sqlite`, which currently fails
# schema migration, so a Postgres URL is passed explicitly here.
store-url ?= postgres://urth:urth@localhost:5432/urth

.PHONY: run-api-server
run-api-server: # Start API server
	@go run ./cmd/api-server --store.url="$(store-url)"

.PHONY: run-asynq-worker
run-asynq-worker: # Start Redis based worker
	@go run ./cmd/asynq-runner

.PHONY: run-api-server-nats
run-api-server-nats: # Start API server using the NATS/JetStream transport
	@go run ./cmd/api-server --store.url="$(store-url)" --transport=nats

# The enrolment token comes from the environment or a file rather than a flag:
# an argument is visible in the process table to every user on the host.
#   export RUNNER_TOKEN=$$(go run ./cmd/urthctl auth-worker -f ./examples/runner.yaml)
.PHONY: run-nats-worker
run-nats-worker: # Start NATS based worker
	@go run ./cmd/nats-worker --client.token="$(RUNNER_TOKEN)"

.PHONY: run-scheduler
run-scheduler: # Start scheduler server
	@echo Not implemented yet....

website/node_modules: 
	@cd website && npm i 

.PHONY: serve-site
serve-site: website/node_modules  # Start Web UI
	@cd website && npm start 

.PHONY: run-redis-podman
run-redis-podman: # Start redis using podman container
	@podman run -p 6379:6379 redis

.PHONY: run-postgres-podman
run-postgres-podman: # Start postgres using podman container
	@podman run -p 5432:5432 -e POSTGRES_USER=urth -e POSTGRES_PASSWORD=urth -e POSTGRES_DB=urth postgres:15

# Single non-replicated server with JetStream: fine for development, explicitly
# not highly available. Production wants three replicas on persistent volumes.
.PHONY: run-nats-podman
run-nats-podman: # Start NATS with JetStream using podman container
	@podman run -p 4222:4222 -p 8222:8222 nats:2.10-alpine -js -m 8222


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
staticcheck-version = 2026.1
govulncheck-version = v1.6.0

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


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	@go test -race -buildvcs ./...

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
	$(RM) ./api-server ./asynq-runner ./urthctl
	$(RM) -dr ./dist $(website-dist)


api-server:
	go build ./cmd/api-server

asynq-runner:
	go build ./cmd/asynq-runner

urthctl:
	go build ./cmd/urthctl

$(website-dist):
	cd website && npm run build

build: api-server $(website-dist) asynq-runner
