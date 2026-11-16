ifneq (,$(wildcard ./.env))
	include .env
	export
endif

# ปรับ DATABASE_URL สำหรับรัน go test จากเครื่อง Host (ชี้ไปที่ localhost แทนชื่อ container postgres)
HOST_DATABASE_URL := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(or $(POSTGRES_PORT),5432)/$(POSTGRES_DB)?sslmode=disable

.PHONY: up down lint test-unit test-contract test-integration test-e2e test

up:
	docker compose up -d --build

down:
	docker compose down -v

lint:
	cd backend && go vet ./...
	cd web && npm run lint && npx tsc --noEmit

test-unit:
	cd backend && go test -v -race ./test -run "Test(OrderID|StateMachine|CalculatePrice)"

test-contract:
	cd backend && DATABASE_URL="$(HOST_DATABASE_URL)" go test -v -race ./test -run "TestOpenAPIContract"

test-integration:
	cd backend && DATABASE_URL="$(HOST_DATABASE_URL)" go test -v -race ./test -run "Test(Invariant|Integration)"

test-e2e:
	cd web && npx playwright test

test: lint test-unit test-contract test-integration test-e2e