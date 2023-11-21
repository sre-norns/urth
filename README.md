# Urth
Probing as a Service

# What?
This project provides a platform to run scripts that monitor infrastructure, devices, and services.

# How
The project consist of 3 main component:
- [api-server](./cmd/api-server/README.md) Rest API server responsible for all resource objects, such as scripts, runners, results etc.
- [runner](./cmd/red-runner) An implementation of async job runner responsible for execution of a script and retuning results back to the API server.
- [scheduler](./cmd/red-scheduler) An implementation of a cron-scheduler that gets a list of all scripts that can be run at a given scheduling interval and creates jobs for runners to execute those scripts.
- [urthctl](./cmd/urthctl/README.md) Command line tool and alternative interface to interact with API service. Inspired by `kubectl`, it similarly allows user to create and inspect resources such as scripts and runners.
- [web UI](./website/README.md) React based Web UI (TBD)

Third party components required to run the service:
- DB:
  - SQLight can be used for local development.
  - Postgres compatible DB is required for Production deployment.
- Job queue:
  - Prefer Redis as job queue, but GCS Pub/sub can be configured.

# Running locally
### Pre-requisites:
- Run Redis locally / in a container. For example using [Podman](https://podman.io/) / [Docker](https://www.docker.com) command:
```bash
> podman run -p 6379:6379 redis
```

### Running API server locally
This is a Go-lang project and as such can be run directly using GO
```bash
> go run ./cmd/api-server
```
After starting a new SQLight3 DB will be created in the current working directory. Thus, if a server is restarted data will no be lost.
By default server runs on `http://localhost:8080`


### Running CLI tool 
By default CLI tool will work with locally running server using default port `:8080`
```bash
> go run ./cmd/urthctl --help
```

In case you need to use `urthctl` to interact with non-local instance of API server, specify address explicitly:
```bash
> go run ./cmd/urthctl --api-server-address="https://urth.sre-norns.com" ... 
```

## Using Makefile
Most repeatable operations to run local deployment are automated using simple [Makefile](./Makefile).

```bash
# Start redis using podman container
> make run-redis-podman

# Start API server
> make run-api-server

# Start scheduler server
> make run-scheduler

# Start Redis based worker
> make run-asynq-worker

# Start Web UI
> make serve-site
```


# Runner
Runners responsibility is to wait for a task to execute a test. Details of the test depend on the job that were picked.
Not all tasks can be picked up by any runner. Runner have _capabilities_ expressed as `labels`. Each job has requirements.
When requirements match runner's _capabilities_ than it can take a job.

## Lifecycle
A new runner must be registered with `API server` first to create a `slot`. This includes generation of an API token that can be used by a `runner` to 
identify itself and talk to the `API server`.
