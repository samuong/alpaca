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
		var rawurl string
		n, err := fmt.Sscanf(trimmed, "PROXY %s", &rawurl)
		if err == nil && n == 1 {
			return url.Parse(rawurl)
		}
		n, err = fmt.Sscanf(trimmed, "SOCKS %s", &rawurl)
		if err == nil && n == 1 {
			msg := "warning: ignoring socks proxy '%s'"
			log.Printf(msg, rawurl)
			return nil, nil
		}
		log.Printf("warning: couldn't parse pac response '%s'", s)
		return nil, err
	}
	return ProxyHandler{pf, &http.Transport{Proxy: proxyFunc}}
}

func (ph ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		u, err := ph.t.Proxy(r)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
		} else if u == nil {
			directConnect(w, r)
		} else {
			proxyConnect(w, r, u.String())
		}
	} else {
		ph.proxyRequest(w, r)
	}
}

func proxyConnect(w http.ResponseWriter, r *http.Request, proxy string) {
	// can't hijack the connection to server, so can't just replay request
	// need to dial and manually write connect header and read response
	proxyUrl, err := url.Parse(proxy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyConn, err := net.Dial("tcp", proxyUrl.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.Write(proxyConn)
	pr := bufio.NewReader(proxyConn)
	res, err := http.ReadResponse(pr, r)
	// should we close the response body, or leave it so that the
	// connection stays open?
	// ...also, might need to check for any buffered data in the reader,
	// and write it to the connection before moving on
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		msg := fmt.Sprintf("Can't hijack connection to %v", r.Host)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(res.StatusCode)
	if res.StatusCode != http.StatusOK {
		return
	}
	client, _, err := h.Hijack()
	if err != nil {
		// The response status has already been sent, so if hijacking
		// fails, we can't return an error status to the client.
		// Instead, log the error and finish up.
		log.Printf("Error hijacking connection to %v", r.Host)
		proxyConn.Close()
		return
	}
	go func() {
		defer proxyConn.Close()
		defer client.Close()
		var wg sync.WaitGroup
		wg.Add(2)
		go transfer(&wg, proxyConn, client)
		go transfer(&wg, client, proxyConn)
		wg.Wait()
	}()
}

func directConnect(w http.ResponseWriter, r *http.Request) {
	// TODO: should probably put a timeout on this
	server, err := net.Dial("tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		msg := fmt.Sprintf("Can't hijack connection to %v", r.Host)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
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

func transfer(wg *sync.WaitGroup, dst, src net.Conn) {
	defer wg.Done()
	_, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Error copying from %v to %v",
			src.RemoteAddr().String(), dst.RemoteAddr().String())
	}
}

func (ph ProxyHandler) proxyRequest(w http.ResponseWriter, r *http.Request) {
	resp, err := ph.t.RoundTrip(r)
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
