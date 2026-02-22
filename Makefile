BINARY := ct-hulhu
MODULE := github.com/TheArqsz/ct-hulhu
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X $(MODULE)/internal/runner.version=$(VERSION)

.PHONY: build clean install test vet

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/ct-hulhu/

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/ct-hulhu/

clean:
	rm -f $(BINARY)

test:
	go test -race ./...

vet:
	go vet ./...
