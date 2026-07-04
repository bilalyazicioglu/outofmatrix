.PHONY: build run tidy vet db-up db-down docker-up docker-down

build:
	go build -o bin/server ./cmd/server

run: build
	./bin/server

tidy:
	go mod tidy

vet:
	go vet ./...

# Local Postgres only (run the Go server on the host during development).
db-up:
	docker compose up -d db

db-down:
	docker compose stop db

# Full stack in containers.
docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
