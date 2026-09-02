GATEWAY_DIR := services/gateway
COMPOSE_FILE := deploy/docker-compose.yml

.PHONY: dev-deps dev-down run-gateway build build-keys test clean

dev-deps:
	docker compose -f $(COMPOSE_FILE) up -d

dev-down:
	docker compose -f $(COMPOSE_FILE) stop

run-gateway:
	cd $(GATEWAY_DIR) && go run ./cmd/server

build:
	cd $(GATEWAY_DIR) && go build -o ../../bin/gateway ./cmd/server

build-keys:
	cd $(GATEWAY_DIR) && go build -o ../../bin/orca-keys ./cmd/orca-keys

test:
	cd $(GATEWAY_DIR) && go test ./...

clean:
	rm -rf bin
