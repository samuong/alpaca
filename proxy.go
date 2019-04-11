package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
)

type ProxyHandler struct {
	transport *http.Transport
}

func NewProxyHandler(pacURL string) ProxyHandler {
	pf := NewProxyFinder(pacURL)
	proxyFunc := func(r *http.Request) (*url.URL, error) { return pf.findProxyForRequest(r) }
	return ProxyHandler{&http.Transport{Proxy: proxyFunc}}
}

func (ph ProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect {
		handleConnect(w, req, ph.transport)
	} else {
		proxyRequest(w, req, ph.transport)
	}
}

func handleConnect(w http.ResponseWriter, req *http.Request, tr *http.Transport) {
	h, ok := w.(http.Hijacker)
	if !ok {
		msg := fmt.Sprintf("Can't hijack connection to %v", req.Host)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	u, err := tr.Proxy(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var server net.Conn
	if u == nil {
		server = connectToServer(w, req, tr)
	} else {
		server = connectViaProxy(w, req, u.Host)
	}
	if server == nil {
		return
	}
	client, _, err := h.Hijack()
	if err != nil {
		// The response status has already been sent, so if hijacking
		// fails, we can't return an error status to the client.
		// Instead, log the error and finish up.
		log.Printf("Error hijacking connection to %v: %s", req.Host, err)
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

func connectViaProxy(w http.ResponseWriter, req *http.Request, proxy string) net.Conn {
	// can't hijack the connection to server, so can't just replay request via a Transport
	// need to dial and manually write connect header and read response
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	req.Write(conn)
	rd := bufio.NewReader(conn)
	resp, err := http.ReadResponse(rd, req)
	// should we close the response body, or leave it so that the
	// connection stays open?
	// ...also, might need to check for any buffered data in the reader,
	// and write it to the connection before moving on
	if err != nil {
		conn.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	w.WriteHeader(resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil
	}
	return conn
}

func connectToServer(w http.ResponseWriter, req *http.Request, tr *http.Transport) net.Conn {
	// TODO: should probably put a timeout on this
	conn, err := net.Dial("tcp", req.Host)
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
		log.Printf("Error copying from %v to %v: %s",
			src.RemoteAddr().String(), dst.RemoteAddr().String(), err)
	}
}

func proxyRequest(w http.ResponseWriter, req *http.Request, tr *http.Transport) {
	resp, err := tr.RoundTrip(req)
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
		log.Printf("Error copying response body from %v: %s", req.Host, err)
		return
	}
}
