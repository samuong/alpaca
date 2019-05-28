#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -e

if [[ "$1" == "fmt" ]]; then
    echo "Running go fmt"
    files=$(gofmt -l $(find . -type f -name '*.go' -not -path "./vendor/*") 2>&1)
    if [ "$files" ]; then
      echo "These files did not pass the gofmt check:"
      echo ${files}
      exit 1
    fi
fi

if [[ "$1" == "test" ]]; then
    echo "Running go test"
    go test ./...
fi
