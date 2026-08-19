.PHONY: build test lint links check clean setup release \
       promote promote-alpha promote-beta promote-rc \
       promote-release

check: lint test links

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
