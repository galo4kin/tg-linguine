BIN := bin/tg-linguine
CMD := ./cmd/bot

.PHONY: build run test lint tidy

build:
	go build -o $(BIN) $(CMD)

run: build
	./$(BIN)

test:
	go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy
