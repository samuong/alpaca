package main

import (
	"bufio"
	"context"
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
	ids       chan uint
}

func NewProxyHandler(proxyFunc func(*http.Request) (*url.URL, error)) ProxyHandler {
	ids := make(chan uint)
	go func() {
		for id := uint(0); ; id++ {
			ids <- id
		}
	}()
	return ProxyHandler{&http.Transport{Proxy: proxyFunc}, ids}
}

func (ph ProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	id := <-ph.ids
	ctx := req.Context()
	ctx = context.WithValue(ctx, "id", id)
	req = req.WithContext(ctx)
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
	defer server.Close()
	client, _, err := h.Hijack()
	if err != nil {
		// The response status has already been sent, so if hijacking
		// fails, we can't return an error status to the client.
		// Instead, log the error and finish up.
		log.Printf("[%d] Error hijacking connection: %s", req.Context().Value("id"), err)
		return
	}
	defer client.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go transfer(req.Context(), server, client, &wg)
	go transfer(req.Context(), client, server, &wg)
	wg.Wait()
}

func connectViaProxy(w http.ResponseWriter, req *http.Request, proxy string) net.Conn {
	// can't hijack the connection to server, so can't just replay request via a Transport
	// need to dial and manually write connect header and read response
	conn, err := net.Dial("tcp", proxy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	err = req.Write(conn)
	if err != nil {
		log.Printf("[%d] Error sending CONNECT request: %v", req.Context().Value("id"), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
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

func transfer(ctx context.Context, dst, src net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	_, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("[%d] Error copying: %v", ctx.Value("id"), err)
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
		log.Printf("[%d] Error copying response body: %s", req.Context().Value("id"), err)
		return
	}
}
