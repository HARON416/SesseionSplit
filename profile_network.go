package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

type ProfileSettingsState struct {
	Settings ProfileSettings `json:"settings"`
	Running  bool            `json:"running"`
}

type ConnectionDiagnostic struct {
	IP           string `json:"ip"`
	Route        string `json:"route"`
	CheckedAt    string `json:"checkedAt"`
	DurationMS   int64  `json:"durationMS"`
	DNSStatus    string `json:"dnsStatus"`
	WebRTCStatus string `json:"webRTCStatus"`
}

func (s *ProfileService) GetProfileSettings(id string) (ProfileSettingsState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ProfileSettingsState{}, errors.New("SessionSplit is closing")
	}
	for _, p := range s.profiles {
		if p.ID == id {
			return ProfileSettingsState{Settings: p.Settings, Running: s.browsers[id] != nil}, nil
		}
	}
	return ProfileSettingsState{}, errors.New("Profile was not found")
}

// CloseProfile releases the browser without removing its persistent data.
func (s *ProfileService) CloseProfile(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("SessionSplit is closing")
	}
	found := false
	for _, p := range s.profiles {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errors.New("Profile was not found")
	}
	if live := s.browsers[id]; live != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := live.browser.Context(ctx).Close(); err != nil {
			live.launcher.Kill()
		}
		cancel()
		live.cancel()
		if live.proxyClose != nil {
			live.proxyClose()
		}
		delete(s.browsers, id)
	}
	return nil
}

// TestProfileConnection only probes the saved route. It does not certify all browser traffic.
func (s *ProfileService) TestProfileConnection(ctx context.Context, id string) (ConnectionDiagnostic, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ConnectionDiagnostic{}, errors.New("SessionSplit is closing")
	}
	var settings ProxySettings
	found := false
	for _, p := range s.profiles {
		if p.ID == id {
			settings = p.Settings.Proxy
			found = true
			break
		}
	}
	store := s.credentials
	if store == nil {
		store = newOSCredentialStore(s.root)
	}
	s.mu.Unlock()
	if !found {
		return ConnectionDiagnostic{}, errors.New("Profile was not found")
	}
	return testProxyConnection(ctx, settings, store, "https://api.ipify.org?format=json")
}

func testProxyConnection(ctx context.Context, settings ProxySettings, store credentialStore, endpoint string) (ConnectionDiagnostic, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return proxyDialContext(ctx, network, address, settings, store)
	}, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 8 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("unexpected redirect") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ConnectionDiagnostic{}, errors.New("Could not prepare the connection test")
	}
	start := time.Now()
	response, err := client.Do(req)
	if err != nil {
		return ConnectionDiagnostic{}, errors.New("Connection test failed. Check the proxy address, credentials, and network connection, then try again")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ConnectionDiagnostic{}, errors.New("The connection test service did not return a successful response. Try again later")
	}
	var result struct {
		IP string `json:"ip"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil || net.ParseIP(result.IP) == nil {
		return ConnectionDiagnostic{}, errors.New("The connection test service returned an invalid IP address")
	}
	route := settings.Mode
	if route == "" {
		route = "direct"
	}
	return ConnectionDiagnostic{IP: result.IP, Route: route, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano), DurationMS: time.Since(start).Milliseconds(), DNSStatus: "Not verified: this request does not prove system-wide DNS isolation", WebRTCStatus: "Not verified: browser WebRTC traffic requires a separate test"}, nil
}
