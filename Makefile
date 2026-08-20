.PHONY: build test lint links check clean setup release quickstart \
       promote promote-alpha promote-beta promote-rc \
       promote-release

check: lint test links

# Build the engine + the adapters the worked example wires, then verify
# wiring with a keyless dry-run. Safe on a fresh clone; no LLM calls.
quickstart:
	mkdir -p e2e/bin
	GOFLAGS=-buildvcs=false go build -o e2e/bin/evol .
	for a in artifact-fs generator-llm executor-apx corpus-fs kb-ctxt; do \
		GOFLAGS=-buildvcs=false go build -o e2e/bin/$$a ./adapters/$$a; \
	done
	e2e/bin/evol run --config e2e/evol.yaml --dry-run --format json

build:
	mkdir -p bin
	go mod tidy
	go build -o bin/evol .

test:
	go test ./...

lint:
	golangci-lint run ./...

links:
	@if command -v lychee >/dev/null 2>&1; then \
		lychee --no-progress .; \
	else \
		echo "lychee not installed; skipping link check"; \
	fi

clean:
	rm -rf bin/ dist/

setup:
	go mod download
	@command -v lychee >/dev/null 2>&1 || cargo install lychee

release:
	goreleaser release --clean

promote:
	@scripts/promote-release.sh

promote-alpha promote-beta promote-rc promote-release:
	@scripts/promote-release.sh $(subst promote-,,$@)
