GATEWAY_DIR := services/gateway
COMPOSE_FILE := deploy/docker-compose.yml

.PHONY: dev-deps dev-down run-gateway build test clean

dev-deps:
	docker compose -f $(COMPOSE_FILE) up -d

dev-down:
	docker compose -f $(COMPOSE_FILE) stop

run-gateway:
	cd $(GATEWAY_DIR) && go run ./cmd/server

build:
	cd $(GATEWAY_DIR) && go build -o ../../bin/gateway ./cmd/server

test:
	cd $(GATEWAY_DIR) && go test ./...

clean:
	rm -rf bin
