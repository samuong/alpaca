# Alpaca

Alpaca is an auto-configuring http/https proxy for command-line tools.

It is currently a proof-of-concept/prototype, and does not yet implement many
important features, such as SOCKS, HTTP/2, and many of the predefined JS
functions that PAC files use.

Contributions are welcome, please reach out to me if you'd like to help!

## Building

The build depends on some third-party libraries. At some point, I'll use Go
Modules or something, but for now, use `goget` to install dependencies:

```sh
$ cd $GOPATH/src
$ git clone https://github.com/samuong/alpaca.git
$ cd alpaca
$ go get github.com/stretchr/testify
$ go get github.com/robertkrimen/otto
$ go get github.com/gobwas/glob
$ go build
```

## Usage

By default, Alpaca will listen on port 3128, and act as a direct proxy (i.e. a
non-caching proxy, without a parent proxy). It can be used by setting
`http_proxy` and `https_proxy`:

```sh
$ ./alpaca >alapca.log 2>&1 &
$ http_proxy=http://localhost:3128
$ https_proxy=http://localhost:3128
$ export http_proxy https_proxy
$ curl -s https://raw.githubusercontent.com/samuong/alpaca/master/README.md | head -n1
# Alpaca
```

It's also possible to use a different port (using the `-p` flag) and use
parent proxies specified by a PAC file (using the `-C` flag):

```sh
$ ./alpaca -p 3129 -C https://somewhere.example.com/proxy.pac >alapca.log 2>&1 &
$ http_proxy=http://localhost:3129
$ https_proxy=http://localhost:3129
$ export http_proxy https_proxy
$ curl -s https://raw.githubusercontent.com/samuong/alpaca/master/README.md | head -n1
# Alpaca
```
