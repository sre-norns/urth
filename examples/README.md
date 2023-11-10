# Examples of working with Urth

## Using API directly

Create your first scenario using curl / http-pie
```bash

curl -X POST 'http://localhost:8080/api/v1/scenarios'  \
-H "Content-Type: application/json" \
--data-binary "@examples/scenario.json"

```

# Using `urhctl` (WIP)
```bash
go run ./cmd/urthctl apply ./examples/scenario.yml
```