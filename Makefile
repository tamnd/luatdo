GO ?= go

.PHONY: all fmt vet test lint build dist clean

all: test build

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -cover ./...

lint:
	golangci-lint run ./...

build:
	$(GO) build -o bin/luatdo ./cmd/luatdo

dist:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o dist/luatdo-linux-amd64 ./cmd/luatdo

clean:
	rm -rf bin dist coverage.out
