.PHONY: all
all: build lint

.PHONY: build
build:
	go build

.PHONY: build-archs
build-archs:
	GOOSNOT="android|ios|js|plan9"; \
	for dist in $$(go tool dist list | egrep -v "^($$GOOSNOT)/|^darwin/amd64$$"); do \
		echo "GOOS=$${dist%/*} GOARCH=$${dist#*/}"; \
		GOOS=$${dist%/*} GOARCH=$${dist#*/} go build; \
	done; \
	rm alpaca

.PHONY: test
test:
	go test ./...

.PHONY: lint
lint:
	golangci-lint run
