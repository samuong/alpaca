package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/armon/go-socks5"
)

// Get network package from socks and transform it in a http proxy package
func httpConnectDialer(proxyHTTPAddr string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := net.Dial(network, proxyHTTPAddr)
		if err != nil {
			return nil, err
		}

		connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", addr, addr)
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			return nil, err
		}

		br := bufio.NewReader(conn)
		status, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if !strings.HasPrefix(status, "HTTP/1.1 200") {
			conn.Close()
			return nil, errors.New("proxy HTTP rejected CONNECT: " + strings.TrimSpace(status))
		}

		for {
			line, err := br.ReadString('\n')
			if err != nil {
				conn.Close()
				return nil, err
			}
			if line == "\r\n" {
				break
			}
		}

		return conn, nil
	}
}

func startSocksServer(proxyHTTPAddr string, a *authenticator) (*socks5.Server, error) {
	var auths []socks5.Authenticator
	if a != nil {
		creds := socks5.StaticCredentials{
			a.username: string(a.hash),
		}
		auths = append(auths, socks5.UserPassAuthenticator{Credentials: creds})
	}

	conf := &socks5.Config{
		AuthMethods: auths,
		Dial:        httpConnectDialer(proxyHTTPAddr),
	}
	srv, err := socks5.New(conf)
	return srv, err
}
