# Makefile is handcrafted to automate repetitive tasks

.PHONY: run-api-server
run-api-server: # Start API server
	@go run ./cmd/api-server

.PHONY: run-asynq-worker
run-asynq-worker: # Start Redis based worker
	@go run ./cmd/asynq-worker

.PHONY: run-scheduler
run-scheduler: # Start scheduler server
	@echo Not implemented yet....

.PHONY: serve-site
serve-site: # Start Web UI
	@cd website && npm start 

.PHONY: run-redis-podman
run-redis-podman: # Start redis using podman container
	@podman run -p 6379:6379 redis

.PHONY: test
test:
	@go test ./...