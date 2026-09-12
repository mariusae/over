GO ?= go

.PHONY: all build test vet fmt lint clean install

all: build

build:
	$(GO) build -o over ./cmd/over

install:
	$(GO) install ./cmd/over

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

# lint runs the checks CI runs: formatting, vet, and tests.
lint: vet
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "not gofmt'd:"; echo "$$out"; exit 1; fi

clean:
	rm -f over
