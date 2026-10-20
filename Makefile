GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet fmt integration install clean
build:
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o bin/tsi ./cmd/tsi

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run gofmt -w ." && exit 1)

# Needs Docker. Starts a real module, so it takes about a minute.
integration:
	$(GO) test -tags integration -timeout 15m -v .

install:
	$(GO) install -ldflags "-X main.version=$(VERSION)" ./cmd/tsi

clean:
	rm -rf bin dist

# Downloads every pinned migration from GitHub and checks its checksum. Needs internet.
.PHONY: network-test
network-test:
	$(GO) test -tags network -run TestPinnedMigrationsMatchUpstream -v ./catalog
