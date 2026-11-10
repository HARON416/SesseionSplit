package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAndroidInstallerProgressAcrossWrites(t *testing.T) {
	setup := &androidSetup{parseInstaller: true}
	setup.stage("installing", 3, "Installing Android tools")
	_, _ = setup.Write([]byte("[=======       ]  23"))
	if setup.status.Percent != -1 {
		t.Fatal("partial percentage was guessed")
	}
	_, _ = setup.Write([]byte(".5% Downloading Android image\r"))
	if setup.status.Percent != 23.5 || setup.status.Phase != "downloading" || setup.status.Detail != "Downloading Android image" {
		t.Fatalf("download status: %+v", setup.status)
	}
	_, _ = setup.Write([]byte("[===========   ]  67% Unzipping system image\r"))
	if setup.status.Percent != 67 || setup.status.Phase != "installing" {
		t.Fatalf("install status: %+v", setup.status)
	}
	_, _ = setup.Write([]byte("[              ]  101% bad output\r"))
	if setup.status.Percent != 67 {
		t.Fatal("invalid percentage accepted")
	}
	setup.stage("checking", 5, "Checking acceleration")
	if setup.status.Percent != -1 || setup.status.Detail != "" || setup.status.ProgressLabel != "" || setup.status.Step != 5 {
		t.Fatal("old operation progress survived stage change")
	}
}
func TestAndroidDownloadReportsActualBytes(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(fmt.Sprint(known), func(t *testing.T) {
			payload := bytes.Repeat([]byte{42}, 256*1024)
			release := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if known {
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				}
				_, _ = w.Write(payload[:len(payload)/2])
				w.(http.Flusher).Flush()
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_, _ = w.Write(payload[len(payload)/2:])
			}))
			defer server.Close()
			setup := &androidSetup{downloadClient: server.Client()}
			service := &AndroidService{setup: setup}
			setup.stage("preparing", 1, "Preparing Java runtime")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- setup.download(ctx, server.URL, fmt.Sprintf("%x", sha256.Sum256(payload)), filepath.Join(t.TempDir(), "download"))
			}()
			deadline := time.Now().Add(3 * time.Second)
			var state AndroidSetupStatus
			for time.Now().Before(deadline) {
				state = service.GetAndroidSetup()
				if state.DownloadedBytes == int64(len(payload)/2) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if state.DownloadedBytes != int64(len(payload)/2) || state.Phase != "downloading" || state.LastActivity == 0 || state.BytesPerSecond <= 0 {
				t.Fatalf("live download status missing: %+v", state)
			}
			if known && (state.TotalBytes != int64(len(payload)) || state.Percent != 50) {
				t.Fatalf("known size progress wrong: %+v", state)
			}
			if !known && (state.TotalBytes != 0 || state.Percent != -1) {
				t.Fatalf("unknown size invented a percentage: %+v", state)
			}
			close(release)
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			state = service.GetAndroidSetup()
			if state.Phase != "verifying" || state.Percent != -1 || state.DownloadedBytes != 0 {
				t.Fatalf("transfer percentage leaked into verification: %+v", state)
			}
		})
	}
}
