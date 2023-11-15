# Examples of working with Urth

## Using API directly

### Create scenario from a file
Create your first scenario using curl / [httpie](https://httpie.io/) / Test HTTP client of your choice
```bash
curl -X POST 'http://localhost:8080/api/v1/scenarios'  \
-H "Content-Type: application/json" \
--data-binary "@examples/scenario.tcp.json"
```

The command above will create a new scenario based on [example resource manifest](./scenaio.tcp.json). Note the system generated ID for the newly created resource.
You can use that ID to modify the example file and update scenario on the server.

### Update scenario object
```bash
curl -X PUT 'http://localhost:8080/api/v1/scenarios/1'  \
-H "Content-Type: application/json" \
--data-binary "@examples/scenario.tcp.json"
```

### List all registered scenarios
```bash
curl 'http://localhost:8080/api/v1/scenarios'
```

### Delete scenario
```bash
curl -X DELETE 'http://localhost:8080/api/v1/scenarios/1'
```

## Trigger a scenario Run manually (outside of normal schedule)
```bash
curl -X PUT 'http://localhost:8080/api/v1/scenarios/1/results'  \
-H "Content-Type: application/json" \
--data-binary "@examples/run.scenario.tcp.json"
```


## Post that a job has been picked up by a worker:
```bash
curl -X POST 'http://localhost:8080/api/v1/scenarios/4/results'  \
-H "Content-Type: application/json" \
--data-binary "@examples/scenario.run.started.json"
```

## Create a slot for a worker
```bash
curl -X POST 'http://localhost:8080/api/v1/runners'  \
-H "Content-Type: application/json" \
--data-binary "@examples/runner.json"
```



## Using `urhctl` (WIP)
```bash
go run ./cmd/urthctl apply ./examples/scenario.yml
```

