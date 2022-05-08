// Copyright 2021 The Alpaca Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
)

// transport creates and manages the lifetime of a net.Conn. Between the time that a remote server
// is dialled, and the connection hijacked or closed, a client can send HTTP requests using the
// RoundTrip method (rather than writing requests and reading responses on the net.Conn).
type transport struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (t *transport) dial(proxy *url.URL) error {
	if err := t.Close(); err != nil {
		return err
	}
	var conn net.Conn
	var err error
	if proxy.Scheme == "https" {
		conn, err = tls.Dial("tcp", proxy.Host, tlsClientConfig)
	} else {
		conn, err = net.Dial("tcp", proxy.Host)
	}
	if err != nil {
		return &net.OpError{Op: "proxyconnect", Net: "tcp", Err: err}
	}
	t.conn = conn
	t.reader = bufio.NewReader(conn)
	return nil
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.conn == nil {
		return nil, errors.New("no connection, can't send request")
	}
	if err := req.Write(t.conn); err != nil {
		return nil, err
	}
	return http.ReadResponse(t.reader, req)
}

func (t *transport) hijack() net.Conn {
	defer func() {
		t.conn = nil
		t.reader = nil
	}()
	return t.conn
}

func (t *transport) Close() error {
	if t.conn == nil {
		return nil
	}
	defer func() {
		t.conn = nil
		t.reader = nil
	}()
	return t.conn.Close()
}
