.PHONY: build build-arm64 test vet
build:
	go build -o bin/bot ./cmd/bot
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/bot-arm64 ./cmd/bot
test:
	go test ./...
vet:
	go vet ./...
