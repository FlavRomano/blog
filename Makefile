TAILWIND_VERSION := v4.1.11
OS   := $(shell uname -s | sed 's/Darwin/macos/; s/Linux/linux/')
ARCH := $(shell uname -m | sed 's/x86_64/x64/; s/aarch64/arm64/')

bin/tailwindcss:
	mkdir -p bin
	curl -fsSL -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(OS)-$(ARCH)
	chmod +x $@

# Compiled CSS is committed so `go build` needs no Tailwind.
css: bin/tailwindcss
	bin/tailwindcss -i web/input.css -o web/static/app.css --minify
seed:
	go run ./cmd/seed

.PHONY: css seed run test lint sqlc build build-arm64
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
