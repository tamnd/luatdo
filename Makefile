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

# dist builds the static binaries for the machines a campaign actually runs on.
# CGO is off, so each one is a single file with no runtime dependency.
dist:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o dist/luatdo-linux-amd64 ./cmd/luatdo
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o dist/luatdo-linux-arm64 ./cmd/luatdo
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o dist/luatdo-darwin-arm64 ./cmd/luatdo
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o dist/luatdo-windows-amd64.exe ./cmd/luatdo

clean:
	rm -rf bin dist coverage.out
