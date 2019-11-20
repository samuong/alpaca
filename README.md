# Alpaca

Alpaca is an HTTP/HTTPS proxy. It is designed for users of Unix tools, who
operate inside networks that use proxy auto-configuration (PAC) files and
require NTLM authentication.

It currently does not yet implement many important features, such as SOCKS,
HTTP/2, and many of the predefined JS functions that PAC files use.
Contributions are welcome, please reach out to me if you'd like to help!

## Installation and Usage

To download and install Alpaca, use:

```sh
$ go get -v -u github.com/samuong/alpaca
```

Then start Alpaca by running the `alpaca` binary.

If your proxy requires NTLM authentication, you'll need to supply your domain and
username (via command-line flags) and a password (via a prompt):

```sh
$ alpaca -d MYDOMAIN -u me
Password (for MYDOMAIN\me):
```

If you're using macOS or GNOME 3+, Alpaca will be able to find the PAC URL from
your system proxy settings. You can also set the URL manually using the `-C`
flag. If no PAC URL is found, Alpaca will act as a direct proxy (i.e. a
non-caching proxy, without a parent proxy).

You can then configure your tools to send requests via Alpaca. Usually this
will require setting the `http_proxy` and `https_proxy` environment variables:

```sh
$ http_proxy=localhost:3128
$ https_proxy=localhost:3128
$ export http_proxy https_proxy
$ curl -s https://raw.githubusercontent.com/samuong/alpaca/master/README.md
# Alpaca
...
```

When moving from, say, a corporate network to a public WiFi network (or
vice-versa), the proxies listed in the PAC script might become unreachable.
When this happens, Alpaca will temporarily bypass the parent proxy and send
requests directly, so there's no need to manually unset/re-set `http_proxy` and
`https_proxy` as you move between networks.
