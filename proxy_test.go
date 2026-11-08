package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func proxyTestSettings(t *testing.T, address, mode string) ProxySettings {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	return ProxySettings{Mode: mode, Host: host, Port: n}
}
func TestHTTPProxyBridgeAuthenticationAndLocalTargets(t *testing.T) {
	requests := make(chan *http.Request, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(context.Background())
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) {
			http.Error(w, "auth", 407)
			return
		}
		io.WriteString(w, "upstream-only")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	settings := proxyTestSettings(t, u.Host, "http")
	settings.Username = "user"
	settings.CredentialID = "ref"
	store := &memoryCredentialStore{values: map[string]string{"ref": "secret"}}
	address, closeProxy, err := startProfileProxy(context.Background(), settings, store)
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	bridge, _ := url.Parse(address)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(bridge)}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://localhost:12345/local-target")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "upstream-only" {
		t.Fatal("HTTP target bypassed upstream")
	}
	select {
	case request := <-requests:
		if request.URL.Host != "localhost:12345" {
			t.Fatal("target changed", request.URL)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream was bypassed")
	}
	closeProxy()
	if _, err := client.Get("http://localhost:12345"); err == nil {
		t.Fatal("closed bridge accepted a request")
	}
}
func TestProxyDialHTTPConnectAndSOCKSRemoteDNS(t *testing.T) {
	for _, mode := range []string{"http", "socks5"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			result := make(chan string, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					result <- err.Error()
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(conn)
				if mode == "http" {
					r, err := http.ReadRequest(reader)
					if err != nil {
						result <- err.Error()
						return
					}
					if r.Method != "CONNECT" || r.Host != "does-not-resolve.invalid:443" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) {
						result <- "bad CONNECT auth or destination"
						return
					}
					io.WriteString(conn, "HTTP/1.1 200 OK\r\n\r\nhello")
				} else {
					greeting := make([]byte, 3)
					if _, err := io.ReadFull(reader, greeting); err != nil || string(greeting) != string([]byte{5, 1, 2}) {
						result <- "bad auth greeting"
						return
					}
					conn.Write([]byte{5, 2})
					auth := make([]byte, 2)
					io.ReadFull(reader, auth)
					username := make([]byte, int(auth[1]))
					io.ReadFull(reader, username)
					length, _ := reader.ReadByte()
					password := make([]byte, int(length))
					io.ReadFull(reader, password)
					if string(username) != "user" || string(password) != "secret" {
						result <- "bad socks auth"
						return
					}
					conn.Write([]byte{1, 0})
					header := make([]byte, 5)
					io.ReadFull(reader, header)
					host := make([]byte, int(header[4]))
					io.ReadFull(reader, host)
					port := make([]byte, 2)
					io.ReadFull(reader, port)
					if header[3] != 3 || string(host) != "does-not-resolve.invalid" || port[0] != 1 || port[1] != 187 {
						result <- "destination did not use remote DNS"
						return
					}
					conn.Write(append([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1}, []byte("hello")...))
				}
				result <- "ok"
			}()
			settings := proxyTestSettings(t, listener.Addr().String(), mode)
			settings.Username = "user"
			settings.CredentialID = "ref"
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, err := proxyDialContext(ctx, "tcp", "does-not-resolve.invalid:443", settings, &memoryCredentialStore{values: map[string]string{"ref": "secret"}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			data := make([]byte, 5)
			if _, err := io.ReadFull(conn, data); err != nil || string(data) != "hello" {
				t.Fatal("tunnel did not preserve data", err)
			}
			if result := <-result; result != "ok" {
				t.Fatal(result)
			}
		})
	}
}
func TestProxyFailureDoesNotReachOrigin(t *testing.T) {
	hit := make(chan bool, 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit <- true }))
	defer origin.Close()
	unavailable, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := unavailable.Addr().String()
	unavailable.Close()
	settings := proxyTestSettings(t, address, "http")
	bridge, closeProxy, err := startProfileProxy(context.Background(), settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	u, _ := url.Parse(bridge)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 502 {
		t.Fatal("proxy failure not surfaced", response.StatusCode)
	}
	originURL, _ := url.Parse(origin.URL)
	if conn, err := proxyDialContext(context.Background(), "tcp", originURL.Host, settings, nil); err == nil {
		conn.Close()
		t.Fatal("failed proxy dial used direct fallback")
	}
	select {
	case <-hit:
		t.Fatal("origin reached without upstream")
	default:
	}
}
func TestProxyTLSAndCancellation(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS proxy was accepted") }))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	settings := proxyTestSettings(t, u.Host, "https")
	if conn, err := proxyDialContext(context.Background(), "tcp", "example.com:443", settings, nil); err == nil {
		conn.Close()
		t.Fatal("untrusted TLS accepted")
	}
	stalled, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	go func() {
		conn, err := stalled.Accept()
		if err == nil {
			defer conn.Close()
			io.Copy(io.Discard, conn)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if conn, err := proxyDialContext(ctx, "tcp", "example.com:443", proxyTestSettings(t, stalled.Addr().String(), "http"), nil); err == nil {
		conn.Close()
		t.Fatal("stalled handshake accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("proxy handshake ignored cancellation")
	}
	_, _, err = startProfileProxy(context.Background(), ProxySettings{Mode: "http", Host: "proxy", Port: 80, CredentialID: "missing"}, &memoryCredentialStore{values: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "keyring") {
		t.Fatal("missing credentials silently ignored", err)
	}
}

func TestProxyBridgeClosesActiveTunnels(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstreamClosed := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(upstreamClosed)
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		request, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		if request.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) {
			t.Error("bridge did not use its cached upstream credential")
			return
		}
		io.WriteString(conn, "HTTP/1.1 200 Connected\r\n\r\n")
		io.Copy(conn, reader)
	}()
	settings := proxyTestSettings(t, listener.Addr().String(), "http")
	settings.Username, settings.CredentialID = "user", "ref"
	store := &memoryCredentialStore{values: map[string]string{"ref": "secret"}}
	address, closeProxy, err := startProfileProxy(context.Background(), settings, store)
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	delete(store.values, "ref") // A live bridge must not query the system keyring again.
	u, _ := url.Parse(address)
	client, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(client, "CONNECT remote.invalid:443 HTTP/1.1\r\nHost: remote.invalid:443\r\n\r\n")
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil || response.StatusCode != 200 {
		t.Fatal("bridge CONNECT failed", err)
	}
	io.WriteString(client, "ping")
	data := make([]byte, 4)
	if _, err := io.ReadFull(reader, data); err != nil || string(data) != "ping" {
		t.Fatal("active tunnel did not transmit", err)
	}
	closeProxy()
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("closed bridge kept client tunnel alive")
	}
	select {
	case <-upstreamClosed:
	case <-time.After(time.Second):
		t.Fatal("closed bridge kept upstream tunnel alive")
	}
}

func TestProxyBridgeWebSocketUpgradeAndHopHeaders(t *testing.T) {
	upstreamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Remove") != "" || r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) || requestedUpgrade(r.Header) != "websocket" {
			t.Error("upgrade request headers were not forwarded safely")
		}
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		defer close(upstreamClosed)
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade, X-Remove\r\nUpgrade: websocket\r\nX-Remove: private\r\nX-Result: kept\r\n\r\nhello")
		buffer.Flush()
		io.Copy(conn, buffer)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	settings := proxyTestSettings(t, u.Host, "http")
	settings.Username = "user"
	settings.CredentialID = "ref"
	address, closeProxy, err := startProfileProxy(context.Background(), settings, &memoryCredentialStore{values: map[string]string{"ref": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	bridge, _ := url.Parse(address)
	client, err := net.Dial("tcp", bridge.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(client, "GET http://remote.invalid/socket HTTP/1.1\r\nHost: remote.invalid\r\nConnection: keep-alive, Upgrade, X-Remove\r\nUpgrade: websocket\r\nX-Remove: private\r\nProxy-Authorization: attacker\r\n\r\n")
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 || requestedUpgrade(response.Header) != "websocket" || response.Header.Get("X-Remove") != "" || response.Header.Get("X-Result") != "kept" {
		t.Fatalf("upgrade response not preserved safely: %+v", response)
	}
	data := make([]byte, 5)
	if _, err := io.ReadFull(reader, data); err != nil || string(data) != "hello" {
		t.Fatal("early upgrade bytes lost", err)
	}
	io.WriteString(client, "reply")
	if _, err := io.ReadFull(reader, data); err != nil || string(data) != "reply" {
		t.Fatal("upgraded connection not bidirectional", err)
	}
	closeProxy()
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("upgrade connection survived bridge shutdown")
	}
	select {
	case <-upstreamClosed:
	case <-time.After(time.Second):
		t.Fatal("upgraded upstream survived bridge shutdown")
	}
}

func TestSOCKSBridgeDoesNotSendCredentialsToOrigin(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- err.Error()
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(conn)
		greeting := make([]byte, 2)
		if _, err := io.ReadFull(reader, greeting); err != nil || greeting[0] != 5 {
			result <- "missing SOCKS authentication"
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, err := io.ReadFull(reader, methods); err != nil || !strings.ContainsRune(string(methods), 2) {
			result <- "missing SOCKS username/password method"
			return
		}
		conn.Write([]byte{5, 2})
		prefix := make([]byte, 2)
		io.ReadFull(reader, prefix)
		username := make([]byte, int(prefix[1]))
		io.ReadFull(reader, username)
		length, _ := reader.ReadByte()
		password := make([]byte, int(length))
		io.ReadFull(reader, password)
		if string(username) != "user" || string(password) != "secret" {
			result <- "wrong SOCKS credential"
			return
		}
		conn.Write([]byte{1, 0})
		header := make([]byte, 5)
		io.ReadFull(reader, header)
		host := make([]byte, int(header[4]))
		io.ReadFull(reader, host)
		port := make([]byte, 2)
		io.ReadFull(reader, port)
		if header[3] != 3 || string(host) != "remote.invalid" {
			result <- "SOCKS HTTP destination DNS was not remote"
			return
		}
		conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 1})
		request, err := http.ReadRequest(reader)
		if err != nil {
			result <- err.Error()
			return
		}
		if request.Header.Get("Proxy-Authorization") != "" || request.URL.User != nil || strings.Contains(request.RequestURI, "secret") {
			result <- "upstream credentials reached origin HTTP request"
			return
		}
		io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		result <- "ok"
	}()
	settings := proxyTestSettings(t, listener.Addr().String(), "socks5")
	settings.Username = "user"
	settings.CredentialID = "ref"
	address, closeProxy, err := startProfileProxy(context.Background(), settings, &memoryCredentialStore{values: map[string]string{"ref": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	u, _ := url.Parse(address)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	request, _ := http.NewRequest("GET", "http://remote.invalid/", nil)
	request.Header.Set("Proxy-Authorization", "attacker")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "ok" {
		t.Fatal("SOCKS HTTP bridge failed", string(data))
	}
	if result := <-result; result != "ok" {
		t.Fatal(result)
	}
}

func TestProxyAuthenticationRejectionDoesNotLeakOrFallBack(t *testing.T) {
	for _, mode := range []string{"http", "socks5"} {
		t.Run(mode, func(t *testing.T) {
			originHit := make(chan struct{}, 1)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originHit <- struct{}{}
				io.WriteString(w, "origin reached")
			}))
			defer origin.Close()
			originURL, _ := url.Parse(origin.URL)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			upstreamDone := make(chan struct{})
			go func() {
				defer close(upstreamDone)
				for range 2 {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					func() {
						defer conn.Close()
						conn.SetDeadline(time.Now().Add(3 * time.Second))
						reader := bufio.NewReader(conn)
						if mode == "http" {
							if _, err := http.ReadRequest(reader); err != nil {
								return
							}
							body := "private upstream body username=user password=secret"
							io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: "+strconv.Itoa(len(body))+"\r\nConnection: close\r\n\r\n"+body)
							return
						}
						header := make([]byte, 2)
						if _, err := io.ReadFull(reader, header); err != nil {
							return
						}
						methods := make([]byte, int(header[1]))
						if _, err := io.ReadFull(reader, methods); err != nil {
							return
						}
						conn.Write([]byte{5, 2})
						if _, err := io.ReadFull(reader, header); err != nil {
							return
						}
						username := make([]byte, int(header[1]))
						if _, err := io.ReadFull(reader, username); err != nil {
							return
						}
						length, err := reader.ReadByte()
						if err != nil {
							return
						}
						password := make([]byte, int(length))
						if _, err := io.ReadFull(reader, password); err != nil {
							return
						}
						conn.Write(append([]byte{1, 1}, []byte("private upstream body username=user password=secret")...))
					}()
				}
			}()
			settings := proxyTestSettings(t, listener.Addr().String(), mode)
			settings.Username = "user"
			settings.CredentialID = "ref"
			store := &memoryCredentialStore{values: map[string]string{"ref": "secret"}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			connection, err := proxyDialContext(ctx, "tcp", originURL.Host, settings, store)
			if err == nil {
				connection.Close()
				t.Fatal("rejected proxy authentication was accepted")
			}
			for _, private := range []string{"secret", "username=user", "private upstream body"} {
				if strings.Contains(err.Error(), private) {
					t.Fatal("authentication error leaked private proxy details", err)
				}
			}
			if mode == "http" && !strings.Contains(err.Error(), "407") {
				t.Fatal("HTTP auth rejection not identified", err)
			}
			if mode == "socks5" && !strings.Contains(err.Error(), "rejected the username or password") {
				t.Fatal("SOCKS auth rejection not identified", err)
			}
			address, closeProxy, err := startProfileProxy(context.Background(), settings, store)
			if err != nil {
				t.Fatal(err)
			}
			defer closeProxy()
			bridge, _ := url.Parse(address)
			client, err := net.DialTimeout("tcp", bridge.Host, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			client.SetDeadline(time.Now().Add(3 * time.Second))
			io.WriteString(client, "CONNECT "+originURL.Host+" HTTP/1.1\r\nHost: "+originURL.Host+"\r\n\r\n")
			response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: "CONNECT"})
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 502 || string(body) != "The configured proxy could not connect to this site\n" {
				t.Fatalf("bridge did not return a generic auth failure: %d %q", response.StatusCode, body)
			}
			select {
			case <-upstreamDone:
			case <-time.After(time.Second):
				t.Fatal("upstream auth rejection did not finish")
			}
			select {
			case <-originHit:
				t.Fatal("rejected authentication fell back directly to the origin")
			default:
			}
		})
	}
}
