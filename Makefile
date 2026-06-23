GO ?= go
WASM_EXEC := $(shell $(GO) env GOROOT)/lib/wasm/wasm_exec.js
PRETTIER ?= npx --yes prettier@3.6.2
WEB_GLOB := 'web/**/*.{js,html,json}'

.PHONY: all build wasm test serve clean fmt-web lint-web goldens

all: build wasm

# Regenerate web/app-access/goldens.json from the resourcematcher golden
# testdata, so the worked examples that pin the engine are browsable in the
# playground alongside the hand-curated samples.json.
goldens:
	$(GO) run ./cmd/gengoldens

# Build the command-line evaluator into ./bin/eval.
build:
	$(GO) build -o bin/eval ./cmd/eval

# Build both WebAssembly modules and refresh the JavaScript support file in
# each web directory.
wasm:
	GOOS=js GOARCH=wasm $(GO) build -o web/labels/eval.wasm ./cmd/labels-wasm
	GOOS=js GOARCH=wasm $(GO) build -o web/app-access/eval.wasm ./cmd/appaccess-wasm
	cp "$(WASM_EXEC)" web/labels/wasm_exec.js
	cp "$(WASM_EXEC)" web/app-access/wasm_exec.js

test:
	$(GO) test ./...

# Serve the web directory at http://localhost:8080. The label evaluator is at
# /labels and the app-access evaluator is at /app-access.
serve: wasm
	$(GO) run ./cmd/serve

clean:
	rm -rf bin web/labels/eval.wasm web/app-access/eval.wasm

# Format the web JavaScript, HTML, and JSON with Prettier.
fmt-web:
	$(PRETTIER) --write $(WEB_GLOB)

# Check that the web sources are Prettier-clean, without rewriting them.
lint-web:
	$(PRETTIER) --check $(WEB_GLOB)
