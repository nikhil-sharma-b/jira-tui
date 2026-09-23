package jira_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadAttachmentStreamsTheContentWithCredentials(t *testing.T) {
	var path, user string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, _, _ = r.BasicAuth()
		w.Write([]byte("PNG bytes"))
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)

	var dst bytes.Buffer
	n, err := c.DownloadAttachment(context.Background(), "10042", &dst)
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	if path != "/rest/api/3/attachment/content/10042" {
		t.Errorf("path = %q, want the v3 attachment content endpoint", path)
	}
	if user == "" {
		t.Error("request carried no credentials")
	}
	if got := dst.String(); got != "PNG bytes" || n != int64(len(got)) {
		t.Errorf("wrote %d bytes %q, want the body and its length", n, got)
	}
}

func TestDownloadAttachmentFailureWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errorMessages":["The attachment does not exist."]}`))
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)

	var dst bytes.Buffer
	_, err := c.DownloadAttachment(context.Background(), "1", &dst)
	if err == nil {
		t.Fatal("DownloadAttachment succeeded on a 404")
	}
	if dst.Len() != 0 {
		t.Errorf("wrote %q into the destination on failure", dst.String())
	}
}
