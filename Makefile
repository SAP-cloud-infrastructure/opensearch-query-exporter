.PHONY: build test cover lint fmt clean

BINARY_NAME = opensearch-query-exporter

build:
	CGO_ENABLED=0 go build -ldflags="-w -s" -o $(BINARY_NAME) ./cmd/exporter

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

fmt:
	go fmt ./...

clean:
	rm -f $(BINARY_NAME) coverage.out coverage.html
