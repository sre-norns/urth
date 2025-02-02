# Urth
Probing as a Service

---
![Build status](https://github.com/sre-norns/urth/actions/workflows/go.yml/badge.svg)

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

## (Dev) using `Procfile`
You can use tools like [foreman](https://github.com/ddollar/foreman) or its clones ([goreman](https://github.com/mattn/goreman), [honcho](https://github.com/nickstenning/honcho), [etc](https://github.com/ddollar/foreman#ports)) to run all application component in one go:
```bash
> goreman -b 8080 start 
```

## Running individual components
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


# Architecture (How is supposed to work)
Entire system consist of the following main components:
* API server - responsible for management of all resources and giving jobs to workers. Production deployment is expected to run multiple replicas for reliability.
* Worker - a process located at a vantage point, from which a test should be performed. 
* Job queue - a mechanism for API server to post jobs for workers.
* Web UI - React web application to interact with the system: see existing resources and create new ones.

## API Server
API server manges all entities modeled by the system:
 * scenarios - object that users create and schedule to run
 * Workers - registration details and permissions to perform jobs.
 * Results - a record of jobs performed.
 * Artifacts - data produced by a worked in the course of performing a job.

## Worker
Worker's responsibility is to perform a job assigned by the API server. It waits for a job in the job queue and when one becomes available it picks it up at attempts to perform it. Details of the job depend on the job that were picked.
Not all tasks can be picked up by any runner. Runner have _capabilities_ expressed as `labels` and each job has a  set of requirements that a worker must satisfy in order to perform it.
When requirements match workers's _capabilities_ than it can take and perform a job.

### Lifecycle
A new worker must first be registered with the `API server` by creating a _slot_. Creation of such _slot_ generates token that a `worker` instance must present to the `API server` issuer as part of initial configuration process. Presentation of a valid token notifies `API server` that a worker is ready to pick up jobs. After successful authorization, a worker joins a job queue and awaits.
When a job is available, worker picks it up notifies `API server` that it picked the job. This constitutes authorization of a particular instance of a worker to the API server for a specific job. At this point (WIP) API server will check that worker is indeed authorized to perform the job in question and if successful will issue a short-living token that must be used by the worker to post results back to the API server. Token life-time is chosen by the server to be approximately the maximum allowed run-duration of the task + some buffer time to account communication delays. This mechanism is designed to prevent workers from replaying jobs or posting already existing job results after restart and restore. 

# Running demo:

```shell
##------------------------------
# Spin-up test infrastructure:
##------------------------------
# Start a PostgresDB. For example using docker/podman, in a separate terminal
podman run -p 5432:5432 -e POSTGRES_PASSWORD=<db_password> -e POSTGRES_USER=dbusername postgres:15
# Start a redis instance for job queuing. For example using docker/podman, in a separate terminal
podman run -p 6379:6379 redis

##------------------------------
# Spin-up services:
##------------------------------

# Start API server in a separate terminal and point it to the Postgres instance. check
go run ./cmd/api-server --store.url="postgres://dbusername:<db_password>@localhost:5432"  

# Start WebUI using dev server, in a separate terminal
cd website && npm start

##------------------------------
# Create some resources
##------------------------------

# Create an job runner. See ./examples dir for different resources manifests 
go run ./cmd/urthctl apply ./examples/runner.json

# List all currently registered runners to insure your new example runner has been created.
go run ./cmd/urthctl get runners -o wide  

# Create your first test scenario
go run ./cmd/urthctl apply ./examples/scenario.yml 

# List all currently registered scenarios to ensure your newly created one is correct
go run ./cmd/urthctl get scenarios -o wide


##------------------------------
## Start some workers and connect them to the job queue
##------------------------------

# Start an instance of runner. Note the token is generated for this example to authorize as `./examples/runner.json`
go run ./cmd/asynq-runner --client.token="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJleGFtcGxlLXJ1bm5lci1qc29uIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.nXfHt_2ejYA6XinTuL2MLCojq54FJmLLy9EnRE-T6xU"

##------------------------------
## Trigger a scenario run manually
##------------------------------
# Open your favorite browser and navigate to 'http://localhost:3000/'
# Find `basic-rest-self-prober-http` in the list and press Play [>] button on the right hand side.
# Note: UI refresh is still not implemented so you'll have to refresh the page.

# Inspect scenarios:
go run ./cmd/urthctl get scenarios -o wide


# Inspect run results:
> http ':8080/api/v1/scenarios/basic-rest-self-prober-http/results'
```