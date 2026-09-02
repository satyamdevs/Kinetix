GATEWAY_DIR := services/gateway
DASHBOARD_DIR := services/dashboard
COMPOSE_FILE := deploy/docker-compose.yml

.PHONY: dev-deps dev-down run-gateway build build-keys test bench integration-test clean dashboard-install dashboard-dev dashboard-build

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

dashboard-install:
	cd $(DASHBOARD_DIR) && npm install

dashboard-dev:
	cd $(DASHBOARD_DIR) && npm run dev

dashboard-build:
	cd $(DASHBOARD_DIR) && npm run build

test:
	cd $(GATEWAY_DIR) && go test ./... -count=1

integration-test:
	cd $(GATEWAY_DIR) && go test -v -run 'TestIntegration|TestKeyAdmin|TestFailover|TestStreamChat|TestChatCompletions' ./internal/...

bench:
	cd $(GATEWAY_DIR) && go test ./internal/api/ -run '^$$' -bench BenchmarkOverhead -benchmem

clean:
	rm -rf bin
