.PHONY: build test vet
build:
	go build -o bin/bot ./cmd/bot
test:
	go test ./...
vet:
	go vet ./...
