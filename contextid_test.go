package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func getIdFromRequest(t *testing.T, server *httptest.Server) uint {
	res, err := http.Get(server.URL)
	assert.NoError(t, err)
	b, err := ioutil.ReadAll(res.Body)
	assert.NoError(t, err)
	id, err := strconv.ParseUint(string(b), 10, 64)
	assert.NoError(t, err)
	return uint(id)
}

func TestContextId(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Context().Value("id")
		_, err := w.Write([]byte(strconv.FormatUint(uint64(id.(uint)), 10)))
		assert.NoError(t, err)
	})
	server := httptest.NewServer(AddContextID(handler))
	defer server.Close()
	id0 := getIdFromRequest(t, server)
	assert.Equal(t, id0, uint(0))
	id1 := getIdFromRequest(t, server)
	assert.Equal(t, id1, uint(1))
}
