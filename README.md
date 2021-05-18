# Alpaca

![Latest Tag][2] ![GitHub Workflow Status][3] ![GitHub Releases][4]

Alpaca is a local HTTP proxy for command-line tools. It supports proxy
auto-configuration (PAC) files and NTLM authentication.

## Install using Homebrew

If you're using macOS and use [Homebrew](https://brew.sh/), you can install
using:

```sh
$ brew tap samuong/alpaca
$ brew install samuong/alpaca/alpaca
```

Launch Alpaca by running `alpaca`, or by using `brew services start alpaca`.

## Install using Go

If you've got the [Go](https://golang.org/cmd/go/) tool installed, you can
install using:

```sh
$ go get -v -u github.com/samuong/alpaca
```

## Download Binary

Alpaca can be downloaded from the [GitHub releases page][1].

## Usage

Start Alpaca by running the `alpaca` binary.

On macOS and GNOME systems, Alpaca uses the PAC URL from your system settings.
If you'd like to override this, or if Alpaca fails to detect your settings, you
can set this manually using the `-C` flag.

If you use [NoMAD](https://nomad.menu/products/#nomad) and have configured it
to [use the keychain](https://nomad.menu/help/keychain-usage/), Alpaca will use
these credentials to authenticate to any NTLM challenge from your proxies. You
can also supply your domain and username (via command-line flags) and a
password (via a prompt):

```sh
$ alpaca -d MYDOMAIN -u me
Password (for MYDOMAIN\me):
```

You also need to configure your tools to send requests via Alpaca. Usually this
will require setting the `http_proxy` and `https_proxy` environment variables:

```sh
$ export http_proxy=http://localhost:3128
$ export https_proxy=http://localhost:3128
$ curl -s https://raw.githubusercontent.com/samuong/alpaca/master/README.md
# Alpaca
...
```

When moving from, say, a corporate network to a public WiFi network (or
vice-versa), the proxies listed in the PAC script might become unreachable.
When this happens, Alpaca will temporarily bypass the parent proxy and send
requests directly, so there's no need to manually unset/re-set `http_proxy` and
`https_proxy` as you move between networks.

## Non-interactive launch

If you want to use Alpaca without any interactive password prompt, you can store
your NTLM credentials (domain, username and MD4-hashed password) in an
environment variable called `$NTLM_CREDENTIALS`. You can use the `-H` flag to
generate this value:

```sh
$ ./alpaca -d MYDOMAIN -u me -H
Password (for MYDOMAIN\me):
# Add this to your ~/.profile (or equivalent) and restart your shell
NTLM_CREDENTIALS="me@MYDOMAIN:823893adfad2cda6e1a414f3ebdf58f7"; export NTLM_CREDENTIALS
```

Note that this hash is *not* cryptographically secure; it's just meant to stop
people from being able to read your password with a quick glance.

Once you've set this environment variable, you can start Alpaca by running
`./alpaca`.

[1]: https://github.com/samuong/alpaca/releases
[2]: https://img.shields.io/github/v/tag/samuong/alpaca.svg?logo=github&label=latest
[3]: https://img.shields.io/github/workflow/status/samuong/alpaca/Continuous%20Integration/master
[4]: https://img.shields.io/github/downloads/samuong/alpaca/latest/total
