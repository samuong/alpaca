// Copyright 2021, 2024 The Alpaca Authors
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
	"errors"
	"net"
	"net/http"
)

// transport takes ownership of a net.Conn and allows it to be used as an
// http.RoundTripper (rather than writing requests and reading responses
// directly on the net.Conn).
type transport struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (t *transport) swap(next net.Conn) net.Conn {
	prev := t.conn
	t.conn = next
	t.reader = bufio.NewReader(next)
	return prev
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

func (t *transport) release() net.Conn {
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
