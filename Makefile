.PHONY: build run tidy vet web web-dev db-up db-down docker-up docker-down

build:
	go build -o bin/server ./cmd/server

# Serves whatever is currently in static/ — run `make web` after frontend changes.
run: build
	./bin/server

web:
	cd web && npm install && npm run build

# Vite dev server on :5173 with hot reload; proxies /api and /ws to :8080.
web-dev:
	cd web && npm install && npm run dev

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
