BINARY := check-talos
MODULE := github.com/DLAKE-IO/check-talos
BUILD_DIR := build

GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: build test lint clean

build:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/$(BINARY) ./cmd/check-talos

test:
	$(GO) test -race -count=1 ./...

lint:
	$(GO) vet ./...

clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache -testcache
