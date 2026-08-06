.PHONY: build run vet test

build:
	go build -o bin/billy-tui ./...

run:
	./bin/billy-tui

vet:
	go vet ./...

test: vet
	go test ./...
