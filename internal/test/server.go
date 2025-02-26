package test

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// TestServer is a wrapper around httptest.Server.
type TestServer struct {
	*httptest.Server
}

// NewHTTPSServer creates a new TestServer with a TLS certificate.
// The server is automatically closed when the test ends.
func NewHTTPSServer(tb testing.TB, handle func(w http.ResponseWriter, r *http.Request)) TestServer {
	ts := httptest.NewTLSServer(http.HandlerFunc(handle))
	tb.Cleanup(func() { ts.Close() })
	return TestServer{ts}
}

// NewHTTPServer creates a new TestServer without TLS.
// The server is automatically closed when the test ends.
func NewHTTPServer(tb testing.TB, handle func(w http.ResponseWriter, r *http.Request)) TestServer {
	ts := httptest.NewServer(http.HandlerFunc(handle))
	tb.Cleanup(func() { ts.Close() })
	return TestServer{ts}
}

// NewHTTPSServerForJSON creates a new TestServer that responds with a JSON body.
func NewHTTPSServerForJSON(tb testing.TB, status int, resp any) TestServer {
	return NewHTTPSServer(tb, func(w http.ResponseWriter, r *http.Request) {
		respJson, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			tb.Errorf("Failed to marshal response: %v", err)
			return
		}

		w.WriteHeader(status)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(respJson); err != nil {
			tb.Error("Failed to write response from test sever", err)
		}
	})
}

// CAPEM returns the PEM-encoded certificate of the server.
func (s TestServer) CAPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: s.Certificate().Raw,
	})
}

// Port returns the port the server is listening on.
func (s TestServer) Port() (int, error) {
	serverURL, err := url.Parse(s.URL)
	if err != nil {
		return 0, err
	}

	serverPort, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		return 0, err
	}

	return serverPort, nil
}
