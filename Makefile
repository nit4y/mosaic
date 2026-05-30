GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: build fmt fmt-check vet test race lint check

build:
	$(GO) build ./...

fmt:
	$(GO) fmt ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

lint:
	$(GOLANGCI_LINT) run

# Run everything CI runs.
check: fmt-check vet lint race
