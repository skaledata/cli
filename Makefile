.PHONY: build install test

build: ## Build the skale binary
	go build -ldflags "-X github.com/skaledata/cli/cmd.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/skale .

install: ## Install skale to /usr/local/bin
	go build -ldflags "-X github.com/skaledata/cli/cmd.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o /usr/local/bin/skale .

test: ## Run tests
	go test -v ./...
