# DB Backup Tool - Makefile

BINARY=db-backup-tool
VERSION=$(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS=-s -w -X main.version=$(VERSION)

.PHONY: all build windows linux mac clean run

all: windows linux mac

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY).exe .

linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-linux .

mac:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-mac .

arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-arm64 .

run:
	go run .

clean:
	rm -f $(BINARY) $(BINARY).exe $(BINARY)-linux $(BINARY)-mac $(BINARY)-linux-arm64

dev:
	go run .
