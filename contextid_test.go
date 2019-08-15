package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getIDFromRequest(t *testing.T, server *httptest.Server) uint {
	res, err := http.Get(server.URL)
	require.NoError(t, err)
	b, err := ioutil.ReadAll(res.Body)
	require.NoError(t, err)
	id, err := strconv.ParseUint(string(b), 10, 64)
	require.NoError(t, err)
	return uint(id)
}

func TestContextID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := r.Context().Value("id").(uint)
		assert.True(t, ok, "Unexpected type for context id value")
		_, err := w.Write([]byte(strconv.FormatUint(uint64(id), 10)))
		require.NoError(t, err)
	})
	server := httptest.NewServer(AddContextID(handler))
	defer server.Close()
	assert.Equal(t, uint(0), getIDFromRequest(t, server))
	assert.Equal(t, uint(1), getIDFromRequest(t, server))
}
