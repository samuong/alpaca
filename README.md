# Alpaca

![Alpaca Logo](assets/alpaca-small.png)

![Latest Tag][2] ![GitHub Workflow Status][3] ![GitHub Releases][4]

Alpaca is a local HTTP proxy for command-line tools. It supports proxy
auto-configuration (PAC) files and NTLM authentication.
![alt text](assets/alpaca-banner.png)

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
$ go install github.com/samuong/alpaca/v2@latest
```

## Download Binary

Alpaca can be downloaded from the [GitHub releases page][1].

## Install from distribution packages

[![Packaging status](https://repology.org/badge/vertical-allrepos/alpaca-proxy.svg)](https://repology.org/project/alpaca-proxy/versions)

## Usage

Start Alpaca by running the `alpaca` binary.

If the proxy server requires valid authentication credentials, you can provide them by means of:

- the shell prompt, if `-d` is passed,
- the shell environment, if `NTLM_CREDENTIALS` is set,
- the system keyring (macOS, Windows and Linux/GNOME supported), if none of the above applies.

Otherwise, the authentication with proxy will be simply ignored.

### Shell Prompt

You can also supply your domain and username (via command-line flags) and a
password (via a prompt):

```sh
$ alpaca -d MYDOMAIN -u me
Password (for MYDOMAIN\me):
```

### Non-interactive launch

If you want to use Alpaca without any interactive password prompt, you can store
your NTLM credentials (domain, username and MD4-hashed password) in an
environment variable called `$NTLM_CREDENTIALS`. You can use the `-H` flag to
generate this value:

```sh
$ ./alpaca -d MYDOMAIN -u me -H
# Add this to your ~/.profile (or equivalent) and restart your shell
NTLM_CREDENTIALS="me@MYDOMAIN:823893adfad2cda6e1a414f3ebdf58f7"; export NTLM_CREDENTIALS
```

Note that this hash is *not* cryptographically secure; it's just meant to stop
people from being able to read your password with a quick glance.

Once you've set this environment variable, you can start Alpaca by running
`./alpaca`.

### Keyring

On macOS, if you use [NoMAD](https://nomad.menu/products/#nomad) and have configured it
to [use the keychain](https://nomad.menu/help/keychain-usage/), Alpaca will use
these credentials to authenticate to any NTLM challenge from your proxies.

On Windows and Linux/GNOME you will need some extra work to persist the username (`NTLM_USERNAME`) and the domain (`NTLM_DOMAIN`) 
in the shell environoment, while the password in the system keyring. Alpaca will read the password from the system keyring 
(in the `login` collection) using the attributes `service=alpaca` and `username=$NTLM_USERNAME`.

To store the password in the GNOME keyring, do the following:
```bash
$ export NTLM_USERNAME=<your-username-here>
$ export NTLM_DOMAIN=<your-domain-here>
$ sudo apt install libsecret-tools
$ secret-tool store -c login -l "NTLM credentials" "service" "alpaca" "username" $NTLM_USERNAME
Password:
# Type your password, then run
$ alpaca
```

On macOS and Linux/GNOME systems, Alpaca uses the PAC URL from your system settings.
If you'd like to override this, or if Alpaca fails to detect your settings, you
can set this manually using the `-C` flag.

---

### Proxy

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

[1]: https://github.com/samuong/alpaca/releases
[2]: https://img.shields.io/github/v/tag/samuong/alpaca.svg?logo=github&label=latest
[3]: https://img.shields.io/github/actions/workflow/status/samuong/alpaca/ci.yml?branch=master
[4]: https://img.shields.io/github/downloads/samuong/alpaca/latest/total
