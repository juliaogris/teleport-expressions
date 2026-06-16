GO ?= go
WASM_EXEC := $(shell $(GO) env GOROOT)/lib/wasm/wasm_exec.js

.PHONY: all build wasm test serve clean

all: build wasm

# Build the command-line evaluator into ./bin/eval.
build:
	$(GO) build -o bin/eval ./cmd/eval

# Build the WebAssembly module and refresh the JavaScript support file.
wasm:
	GOOS=js GOARCH=wasm $(GO) build -o web/eval.wasm ./cmd/wasm
	cp "$(WASM_EXEC)" web/wasm_exec.js

test:
	$(GO) test ./...

# Serve the web directory at http://localhost:8080.
serve: wasm
	$(GO) run ./cmd/serve

clean:
	rm -rf bin web/eval.wasm
