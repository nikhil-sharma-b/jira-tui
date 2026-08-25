package jira_test

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fixtureServer replays recorded Jira responses. Routes map a request path to
// a fixture name under testdata; an unrouted path is a test bug and fails
// loudly rather than returning a plausible 404.
type fixtureServer struct {
	*httptest.Server

	// Requests records what the client actually sent, so tests can assert on
	// authentication and query construction without reaching into the client.
	Requests []*http.Request
}

func newFixtureServer(t *testing.T, routes map[string]string) *fixtureServer {
	t.Helper()
	fs := &fixtureServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.Requests = append(fs.Requests, r)
		name, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("no fixture routed for %s", r.URL.Path)
			http.Error(w, "no fixture", http.StatusInternalServerError)
			return
		}
		writeFixture(t, w, name)
	}))
	t.Cleanup(fs.Server.Close)
	return fs
}

// writeFixture replays one recorded response onto w.
func writeFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	resp := readFixture(t, name)
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
}

// readFixture parses testdata/<name>.http, which is a raw HTTP response.
func readFixture(t *testing.T, name string) *http.Response {
	t.Helper()
	path := filepath.Join("testdata", name+".http")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	// Recorded files are stored with bare newlines so they diff cleanly;
	// net/http wants CRLF between the headers.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	head, body, found := bytes.Cut(raw, []byte("\n\n"))
	if !found {
		t.Fatalf("fixture %s has no blank line separating headers from body", path)
	}
	wire := bytes.ReplaceAll(head, []byte("\n"), []byte("\r\n"))
	wire = append(wire, "\r\n\r\n"...)
	wire = append(wire, body...)

	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(wire)), nil)
	if err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	return resp
}
