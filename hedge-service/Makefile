.PHONY: build test lint up down logs migrate-up migrate-down send-test

build:
	go build ./...

test:
	go test ./... -v -count=1 -race

test-cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

up:
	docker-compose up -d --build

down:
	docker-compose down -v

logs:
	docker-compose logs -f hedge-service

migrate-up:
	migrate -path ./migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path ./migrations -database "$(DATABASE_URL)" down 1

# Тестовый webhook — входящий перевод 500 USD
send-test:
	curl -s -X POST http://localhost:8080/webhook/transfer \
		-H "Content-Type: application/json" \
		-d '{ \
			"external_id": "pay-$(shell date +%s)", \
			"client_id": "client-123", \
			"amount": "500.00", \
			"currency": "USD", \
			"wallet_addr": "bc1qxy2kgdygjrsqtzq2n0yrf249..."}' | jq .

# Посмотреть перевод по ID
get-transfer:
	curl -s http://localhost:8080/transfers/$(ID) | jq .

# Посмотреть ордер по ID
get-order:
	curl -s http://localhost:8080/orders/$(ID) | jq .

health:
	curl -s http://localhost:8080/health | jq .
