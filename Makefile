BINARY := miscale
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
# the embedded Info.plist carries the Bluetooth usage description required by TCC
PLIST := $(CURDIR)/cmd/miscale/Info.plist
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -linkmode external -extldflags '-sectcreate __TEXT __info_plist $(PLIST)'"

.PHONY: build test lint clean install

build: lint
	go build $(LDFLAGS) -o $(BINARY) ./cmd/miscale

test:
	go test -v -race -coverprofile=coverage.out ./...

lint:
	golangci-lint run

clean:
	rm -f $(BINARY) coverage.out

install:
	go install $(LDFLAGS) ./cmd/miscale
