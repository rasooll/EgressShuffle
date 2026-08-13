BINARY := bin/egressshuffle
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf 'dev')
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf 'unknown')
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
TOR_REPLICAS ?= 3
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)

.PHONY: build test test-race fmt fmt-check vet check run docker-build compose-up compose-down compose-logs smoke-test clean

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/egressshuffle

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	test -z "$$(gofmt -l cmd internal)"

vet:
	go vet ./...

check: fmt-check test test-race vet build

run:
	go run ./cmd/egressshuffle

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t egressshuffle:local .

compose-up:
	docker compose up --build --scale tor=$(TOR_REPLICAS)

compose-down:
	docker compose down --remove-orphans

compose-logs:
	docker compose logs -f egressshuffle tor

smoke-test:
	./scripts/smoke-test.sh

clean:
	rm -rf bin dist coverage.out coverage.html egressshuffle
