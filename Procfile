api: PORT=$BASE_PORT go run ./cmd/api-server
web_ui: cd website && API_URL="http://${API_HOST}:${BASE_PORT}/" npm start
worker: sleep 2; go run ./cmd/asynq-runner --api-server-address="http://${API_HOST}:${BASE_PORT}/" --api-token=$WORKER_API_TOKEN 
