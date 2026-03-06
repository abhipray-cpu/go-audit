.PHONY: all setup test test-integ test-load test-chaos test-pen test-all lint bench fuzz cover clean

GO ?= go
GOFLAGS ?= -race
FUZZ_TIME ?= 1m

all: lint test

## setup: Install development tools.
setup:
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## test: Run unit tests with race detector.
test:
	$(GO) test $(GOFLAGS) ./domain/... ./app/...

## test-integ: Run integration tests (requires Docker).
test-integ:
	$(GO) test $(GOFLAGS) -tags integration ./adapter/...

## test-load: Run load tests (requires Docker, ~45 min).
test-load:
	$(GO) test -tags loadtest -timeout 45m ./testing/load/...

## test-chaos: Run chaos engineering tests (requires Docker, ~20 min).
test-chaos:
	$(GO) test -tags chaos -timeout 20m ./testing/chaos/...

## test-pen: Run penetration tests.
test-pen:
	$(GO) test -tags pentest ./testing/pentest/...

## test-all: Run all test suites.
test-all: test test-integ test-load test-chaos test-pen

## lint: Run golangci-lint.
lint:
	golangci-lint run --timeout 5m

## bench: Run benchmarks.
bench:
	$(GO) test -bench=. -benchmem -count=3 ./... | tee bench.txt

## fuzz: Run fuzz tests.
fuzz:
	$(GO) test -fuzz=FuzzDiff -fuzztime=$(FUZZ_TIME) ./domain/diff/...
	$(GO) test -fuzz=FuzzSerializer -fuzztime=$(FUZZ_TIME) ./adapter/serializer/...

## cover: Generate coverage report.
cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## clean: Clean test cache and artifacts.
clean:
	$(GO) clean -testcache
	rm -f coverage.out coverage.html bench.txt
