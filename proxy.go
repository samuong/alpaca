package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type ProxyHandler struct {
	pf proxyFinder
	t  *http.Transport
}

type proxyFinder interface {
	FindProxyForURL(u *url.URL) (string, error)
}

type alwaysDirect struct{}

func (d alwaysDirect) FindProxyForURL(u *url.URL) (string, error) {
	return "DIRECT", nil
}

func NewProxyHandler(pf proxyFinder) ProxyHandler {
	proxyFunc := func(r *http.Request) (*url.URL, error) {
		s, err := pf.FindProxyForURL(r.URL)
		if err != nil {
			return nil, err
		}
		ss := strings.Split(s, ";")
		if len(ss) > 1 {
			msg := "warning: ignoring all but first proxy in '%s'"
			log.Printf(msg, s)
		}
		trimmed := strings.TrimSpace(ss[0])
		if trimmed == "DIRECT" {
			return nil, nil
		}
		var host string
		n, err := fmt.Sscanf(trimmed, "PROXY %s", &host)
		if err == nil && n == 1 {
			return &url.URL{Host: host}, nil
		}
		n, err = fmt.Sscanf(trimmed, "SOCKS %s", &host)
		if err == nil && n == 1 {
			msg := "warning: ignoring socks proxy '%s'"
			log.Printf(msg, host)
			return nil, nil
		}
		log.Printf("warning: couldn't parse pac response '%s'", s)
		return nil, err
	}
	return ProxyHandler{pf, &http.Transport{Proxy: proxyFunc}}
}

func (ph ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		handleConnect(w, r, ph.t)
	} else {
		proxyRequest(w, r, ph.t)
	}
}

func handleConnect(w http.ResponseWriter, r *http.Request, t *http.Transport) {
	h, ok := w.(http.Hijacker)
	if !ok {
		msg := fmt.Sprintf("Can't hijack connection to %v", r.Host)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	u, err := t.Proxy(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var server net.Conn
	if u == nil {
		server = connectToServer(w, r, t)
	} else {
		server = connectViaProxy(w, r, u.Host)
	}
	if server == nil {
		return
	}
	client, _, err := h.Hijack()
	if err != nil {
		// The response status has already been sent, so if hijacking
		// fails, we can't return an error status to the client.
		// Instead, log the error and finish up.
		log.Printf("Error hijacking connection to %v", r.Host)
		server.Close()
		return
	}
	go func() {
		defer server.Close()
		defer client.Close()
		var wg sync.WaitGroup
		wg.Add(2)
		go transfer(&wg, server, client)
		go transfer(&wg, client, server)
		wg.Wait()
	}()
}

func connectViaProxy(w http.ResponseWriter, r *http.Request, proxy string) net.Conn {
	// can't hijack the connection to server, so can't just replay request via a Transport
	// need to dial and manually write connect header and read response
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	r.Write(conn)
	pr := bufio.NewReader(conn)
	res, err := http.ReadResponse(pr, r)
	// should we close the response body, or leave it so that the
	// connection stays open?
	// ...also, might need to check for any buffered data in the reader,
	// and write it to the connection before moving on
	if err != nil {
		conn.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(res.StatusCode)
	if res.StatusCode != http.StatusOK {
		conn.Close()
		return nil
	}
	return conn
}

func connectToServer(w http.ResponseWriter, r *http.Request, t *http.Transport) net.Conn {
	// TODO: should probably put a timeout on this
	conn, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(http.StatusOK)
	return conn
}

func transfer(wg *sync.WaitGroup, dst, src net.Conn) {
	defer wg.Done()
	_, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Error copying from %v to %v",
			src.RemoteAddr().String(), dst.RemoteAddr().String())
	}
}

func proxyRequest(w http.ResponseWriter, r *http.Request, t *http.Transport) {
	resp, err := t.RoundTrip(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	// TODO: Don't retransmit hop-by-hop headers.
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers#hbh
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		// The response status has already been sent, so if copying
		// fails, we can't return an error status to the client.
		// Instead, log the error.
		log.Printf("Error copying response body from %v", r.Host)
		return
	}
}
