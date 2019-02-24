# Alpaca

An auto-configuring http/https proxy for command-line tools.

## Installation

The build depends on some third-party libraries. At some point, I'll use Go Modules or something, but for now, use `goget` to install dependencies:

```sh
$ go get github.com/stretchr/testify
$ go get github.com/robertkrimen/otto
$ go get github.com/gobwas/glob
$ go build
$ ./alpaca
```

## Usage

Alpaca will listen on port 3128 by default, and can be used by setting `http_proxy` and `https_proxy`:

```sh
$ http_proxy=http://localhost:3128
$ https_proxy=http://localhost:3128
$ export http_proxy https_proxy
$ curl -s https://raw.githubusercontent.com/samuong/alpaca/master/README.md | head -n1
# Alpaca
```
