.PHONY: run test lint sqlc build build-arm64
run:
	go run ./cmd/blog
test:
	go test -race ./...
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
build:
	CGO_ENABLED=0 go build -o bin/blog ./cmd/blog
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/blog-linux-arm64 ./cmd/blog
