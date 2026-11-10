package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParallelArtifactRanges(t *testing.T) {
	payload := bytes.Repeat([]byte("verified runtime bytes"), 10000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "artifact", time.Time{}, bytes.NewReader(payload))
	}))
	defer server.Close()
	file, err := os.Create(filepath.Join(t.TempDir(), "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = fetchRanges(ctx, server.URL, file, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	if _, err = file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	result, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(payload, result) {
		t.Fatal("parallel ranges did not reconstruct original artifact", err)
	}
}
func TestParallelRangesRejectIgnoredRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not a partial response")) }))
	defer server.Close()
	file, err := os.Create(filepath.Join(t.TempDir(), "artifact"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = fetchRanges(ctx, server.URL, file, 100); err == nil {
		t.Fatal("unexpected range response accepted")
	}
}
