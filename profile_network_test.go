package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionDiagnosticReportsOnlyObservedRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials reached destination")
		}
		_, _ = w.Write([]byte(`{"ip":"203.0.113.9"}`))
	}))
	defer server.Close()
	result, err := testProxyConnection(context.Background(), ProxySettings{}, nil, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if result.IP != "203.0.113.9" || result.Route != "direct" || result.CheckedAt == "" {
		t.Fatalf("invalid diagnostic: %+v", result)
	}
	if !strings.Contains(result.DNSStatus, "Not verified") || !strings.Contains(result.WebRTCStatus, "Not verified") {
		t.Fatal("diagnostic overstated coverage")
	}
}

func TestConnectionDiagnosticRejectsInvalidResponsesAndCancellation(t *testing.T) {
	for _, body := range []string{`{"ip":"not-an-ip"}`, `<html>private information</html>`, strings.Repeat("x", 5000)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := testProxyConnection(context.Background(), ProxySettings{}, nil, server.URL)
		server.Close()
		if err == nil || strings.Contains(err.Error(), body) {
			t.Fatal("invalid diagnostic response was accepted or exposed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testProxyConnection(ctx, ProxySettings{}, nil, "https://example.invalid"); err == nil {
		t.Fatal("canceled diagnostic succeeded")
	}
}

func TestProfileSettingsStateAndMissingProfile(t *testing.T) {
	s, err := newProfileService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProfile("Work")
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.GetProfileSettings(p.ID)
	if err != nil || state.Running || state.Settings != p.Settings {
		t.Fatal("wrong profile settings", err)
	}
	if err = s.CloseProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetProfileSettings("missing"); err == nil {
		t.Fatal("missing profile accepted")
	}
	if err = s.CloseProfile("missing"); err == nil {
		t.Fatal("missing profile closed")
	}
}
