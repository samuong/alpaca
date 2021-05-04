.PHONY: alpaca
alpaca:
	go build

.PHONY: lint
lint:
	golangci-lint run

.PHONY: coverage.txt
coverage.txt:
	go-acc ./...

coverage.html: coverage.txt
	go tool cover -html=coverage.txt -o coverage.html

.PHONY: test
test: coverage.html lint
	xdg-open coverage.html

clean:
	rm -f alpaca coverage.txt coverage.html
