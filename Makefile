.PHONY: build run test

build:
	go build -o bin/billy-tui ./...

run:
	./bin/billy-tui

test:
	go test ./...
