.PHONY: build test check run interop
build:
	go build -o gateway ./cmd/gateway
run:
	go run ./cmd/gateway
test:
	go test -race ./...
check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go build ./...
	go test -race ./...
interop: build
	python tests/interop/check.py --binary ./gateway
