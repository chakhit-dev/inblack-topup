.PHONY: test test-unit up down logs

# รัน Test ทั้งหมด (Unit + Integration + Concurrency + RBAC) ในคำสั่งเดียว
test:
	@echo "==> Starting api-1, api-2 & worker in test mode..."
	@APP_ENV=test docker compose up -d api-1 api-2 worker proxy
	@echo "==> Waiting for API readyz..."
	@for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		if curl -sf http://localhost:8080/readyz > /dev/null; then break; fi; \
		sleep 1; \
	done
	@echo "==> Running all Go test suites..."
	@cd backend && \
		set -a && [ -f ../.env ] && source ../.env && set +a && \
		TEST_API_URL=http://localhost:8080/api/v1 go test -v -count=1 ./test/... ; \
		TEST_EXIT=$$?; \
		cd .. && APP_ENV=development docker compose up -d api-1 api-2 worker; \
		exit $$TEST_EXIT

# รันเฉพาะ Unit Tests (ไม่ต้องเปิด Docker)
test-unit:
	@cd backend && go test -v -count=1 -run "TestOrderID|TestStateMachine|TestCalculatePrice" ./test/...

up:
	@docker compose up -d --build

down:
	@docker compose down