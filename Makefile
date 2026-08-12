.PHONY: build run vet test

build:
	go build -o bin/billy-tui ./...

# Launch against the live Unix socket so the kernel's peer credentials recognise the
# operator (uid 1000 -> Principal 'operator'). Plain TCP (the bare default addr) carries
# no credentials, so Billy treats you as an unverified visitor and declines. Rebuilds first
# so the socket transport (P10) is always in the binary. Override the target: make run ADDR=...
ADDR ?= unix:///home/billyb/.billy/sock/billy.sock
run: build
	./bin/billy-tui --addr "$(ADDR)"

vet:
	go vet ./...

test: vet
	go test ./...
