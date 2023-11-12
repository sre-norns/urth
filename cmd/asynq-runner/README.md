# Urth Async job runner
A simple worker that picks up job from a Redis-message broker and executes scripts.
Each execution produces artifacts: such as run results, logs, traces, and metrics. This artifacts are pushed back to the API service for storage.
