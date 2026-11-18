BIN := bin/vivid

# Тестовая база: тесты с базой пропускаются, если адрес не задан.
TEST_DB ?= postgres://postgres@localhost:5432/vivid_test?sslmode=disable

.PHONY: build run fmt vet test test-db test-race tidy docker-up docker-down

build:
	go build -trimpath -o $(BIN) ./cmd/vivid

run:
	go run ./cmd/vivid

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

test:
	go test ./...
	node --test internal/bot/widgets/interface.test.cjs internal/web/static/*.test.cjs

# Тесты с настоящей базой: схемы, миграции, выдача заказов.
test-db:
	REQUIRE_TEST_DATABASE=true TEST_DATABASE_URL="$(TEST_DB)" go test ./...
	node --test internal/bot/widgets/interface.test.cjs internal/web/static/*.test.cjs

test-race:
	REQUIRE_TEST_DATABASE=true TEST_DATABASE_URL="$(TEST_DB)" go test -race ./...

tidy:
	go mod tidy

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
