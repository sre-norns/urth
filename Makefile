# Makefile is handcrafted to automate repetitive tasks

website-dist = website/dist

.PHONY: run-api-server
run-api-server: # Start API server
	@go run ./cmd/api-server

.PHONY: run-asynq-worker
run-asynq-worker: # Start Redis based worker
	@go run ./cmd/asynq-runner

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

.PHONY: test
test:
	@go test ./...


api-server:
	go build ./cmd/api-server

asynq-runner:
	go build ./cmd/asynq-runner

urthctl:
	go build ./cmd/urthctl

$(website-dist):
	cd website && npm run build

build: api-server $(website-dist) asynq-runner

.PHONY: clean
clean:
	rm ./api-server ./asynq-runner ./urthctl
	rm -rd $(website-dist)
