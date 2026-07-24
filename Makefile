.PHONY: fmt vet lint check fix test build hooks

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test ./...

check: fmt vet lint test

fix: fmt
	golangci-lint run --fix ./...

build:
	GOOS=linux GOARCH=amd64 go build -o tinvest ./cmd/tinvest

hooks:
	brew install lefthook && lefthook install
