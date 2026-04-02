GO ?= go
GOCACHE ?= /tmp/offline-sync-agent-gocache
ARGS ?= help

.PHONY: build test fmt tidy run-server run-client help

help:
	@printf '%s\n' \
		'make build       Build all packages' \
		'make test        Run the full test suite' \
		'make fmt         Format Go files' \
		'make tidy        Tidy module files' \
		'make run-server  Run the backend server' \
		'make run-client  Run the CLI client (set ARGS=...)'

build:
	GOCACHE=$(GOCACHE) $(GO) build ./...

test:
	GOCACHE=$(GOCACHE) $(GO) test ./...

fmt:
	$(GO) fmt ./...

tidy:
	GOCACHE=$(GOCACHE) $(GO) mod tidy

run-server:
	GOCACHE=$(GOCACHE) $(GO) run ./backend

run-client:
	GOCACHE=$(GOCACHE) $(GO) run ./cmd $(ARGS)
