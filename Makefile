.PHONY: all
all: build lint

.PHONY: build
build:
	go build

# Build all compiling os-arch combinations just to check for regressions. Output
# binaries get deleted.
.PHONY: all-osarchs
all-osarchs:
	TMPDIR=$${TMPDIR:-/tmp}
	GOOSNOT="android|ios|js|plan9"; \
	for dist in $$(go tool dist list | egrep -v "^($$GOOSNOT)/|^darwin/amd64$$"); do \
		echo "  GOOS=$${dist%/*} GOARCH=$${dist#*/}"; \
		GOOS=$${dist%/*} GOARCH=$${dist#*/} go build -o $$TMPDIR/alpaca && \
			printf '\e[A\e[1;32m✔\e[0m\n'; \
	done; \
	rm $$TMPDIR/alpaca

.PHONY: test
test:
	go test ./...

.PHONY: lint
lint:
	golangci-lint run
