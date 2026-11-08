package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

func proxyPassword(settings ProxySettings, store credentialStore) (string, error) {
	if settings.CredentialID == "" {
		return "", nil
	}
	if store == nil {
		return "", errors.New("The proxy password is unavailable; save it again in profile settings")
	}
	password, err := store.Get(settings.CredentialID)
	if err != nil {
		return "", errors.New("Could not read the proxy password; unlock your system keyring and try again")
	}
	return password, nil
}
func upstreamProxyURL(settings ProxySettings, store credentialStore) (*url.URL, error) {
	if err := validateProxySettings(settings); err != nil {
		return nil, err
	}
	if settings.Mode == "" || settings.Mode == "direct" {
		return nil, nil
	}
	password, err := proxyPassword(settings, store)
	if err != nil {
		return nil, err
	}
	host := strings.Trim(settings.Host, "[]")
	u := &url.URL{Scheme: settings.Mode, Host: net.JoinHostPort(host, strconv.Itoa(settings.Port))}
	if settings.Username != "" || settings.CredentialID != "" {
		u.User = url.UserPassword(settings.Username, password)
	}
	return u, nil
}

// proxyDialContext never retries a failed proxy connection directly. SOCKS5
// destinations are sent as hostnames, so DNS resolution happens at the proxy.
func proxyDialContext(ctx context.Context, network, address string, settings ProxySettings, store credentialStore) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, errors.New("Proxy connections require TCP")
	}
	u, err := upstreamProxyURL(settings, store)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 20 * time.Second}
	if u == nil {
		return dialer.DialContext(ctx, network, address)
	}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, errors.New("Could not connect to the configured proxy")
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	if settings.Mode == "https" {
		secure := tls.Client(conn, &tls.Config{ServerName: strings.Trim(settings.Host, "[]"), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return nil, errors.New("Could not establish a secure connection to the proxy")
		}
		conn = secure
	}
	if settings.Mode == "socks5" {
		if err := socks5Connect(conn, address, u.User); err != nil {
			return nil, err
		}
	} else {
		request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
		if u.User != nil {
			password, _ := u.User.Password()
			request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
		}
		if err := request.Write(conn); err != nil {
			return nil, errors.New("Could not send a connection request to the proxy")
		}
		reader := bufio.NewReader(conn)
		response, err := http.ReadResponse(reader, request)
		if err != nil {
			return nil, errors.New("The proxy returned an invalid connection response")
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("The proxy refused the connection (HTTP %d)", response.StatusCode)
		}
		conn = &bufferedProxyConn{Conn: conn, reader: reader}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	success = true
	return conn, nil
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

// Each running bridge keeps the credential it started with, so opening another
// connection does not prompt the system keyring again.
type proxyCredentialSnapshot struct{ key, password string }

func (s proxyCredentialSnapshot) Get(key string) (string, error) {
	if key != s.key {
		return "", errors.New("Proxy credential was not found")
	}
	return s.password, nil
}
func (proxyCredentialSnapshot) Set(string, string) error {
	return errors.New("Proxy credentials cannot be changed while the browser is running")
}
func (proxyCredentialSnapshot) Delete(string) error {
	return errors.New("Proxy credentials cannot be changed while the browser is running")
}

func (c *bufferedProxyConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func socks5Connect(conn net.Conn, address string, user *url.Userinfo) error {
	methods := []byte{0}
	if user != nil {
		methods = []byte{2}
	}
	if _, err := conn.Write(append([]byte{5, byte(len(methods))}, methods...)); err != nil {
		return errors.New("Could not contact the SOCKS5 proxy")
	}
	var reply [2]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil || reply[0] != 5 {
		return errors.New("The SOCKS5 proxy returned an invalid response")
	}
	if reply[1] != methods[0] {
		return errors.New("The SOCKS5 proxy rejected the authentication method")
	}
	if reply[1] == 2 {
		password, _ := user.Password()
		username := user.Username()
		if len(username) > 255 || len(password) > 255 {
			return errors.New("SOCKS5 credentials must be at most 255 bytes")
		}
		auth := append([]byte{1, byte(len(username))}, []byte(username)...)
		auth = append(auth, byte(len(password)))
		auth = append(auth, []byte(password)...)
		if _, err := conn.Write(auth); err != nil {
			return errors.New("Could not authenticate with the SOCKS5 proxy")
		}
		if _, err := io.ReadFull(conn, reply[:]); err != nil || reply[0] != 1 || reply[1] != 0 {
			return errors.New("The SOCKS5 proxy rejected the username or password")
		}
	}
	host, portString, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("Invalid proxy destination")
	}
	port, err := strconv.Atoi(portString)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("Invalid proxy destination port")
	}
	request := []byte{5, 1, 0}
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			request = append(request, 1)
			request = append(request, ipv4...)
		} else {
			request = append(request, 4)
			request = append(request, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return errors.New("Proxy destination hostname is too long")
		}
		request = append(request, 3, byte(len(host)))
		request = append(request, []byte(host)...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	if _, err := conn.Write(request); err != nil {
		return errors.New("Could not send the SOCKS5 connection request")
	}
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 5 || header[1] != 0 {
		return errors.New("The SOCKS5 proxy refused the connection")
	}
	size := 0
	switch header[3] {
	case 1:
		size = 4
	case 4:
		size = 16
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return err
		}
		size = int(length[0])
	default:
		return errors.New("Invalid SOCKS5 destination response")
	}
	_, err = io.CopyN(io.Discard, conn, int64(size+2))
	return err
}

// startProfileProxy supplies Chrome with an unauthenticated loopback HTTP proxy;
// authentication and DNS for remote targets stay on the upstream connection.
func startProfileProxy(ctx context.Context, settings ProxySettings, store credentialStore) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	upstream, err := upstreamProxyURL(settings, store)
	if err != nil {
		return "", nil, err
	}
	if upstream == nil {
		return "", func() {}, nil
	}
	if settings.CredentialID != "" {
		password, _ := upstream.User.Password()
		store = proxyCredentialSnapshot{key: settings.CredentialID, password: password}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, errors.New("Could not start the profile's local proxy")
	}
	transport := &http.Transport{Proxy: http.ProxyURL(upstream), DialContext: (&net.Dialer{Timeout: 20 * time.Second}).DialContext, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	var mu sync.Mutex
	connections := map[io.Closer]struct{}{}
	closed := false
	track := func(conn io.Closer) bool {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			conn.Close()
			return false
		}
		connections[conn] = struct{}{}
		return true
	}
	untrack := func(conn io.Closer) { mu.Lock(); delete(connections, conn); mu.Unlock() }
	server := &http.Server{ReadHeaderTimeout: 15 * time.Second}
	server.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew {
			track(conn)
		}
		if state == http.StateClosed {
			untrack(conn)
		}
	}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			remote, err := proxyDialContext(r.Context(), "tcp", r.Host, settings, store)
			if err != nil {
				http.Error(w, "The configured proxy could not connect to this site", http.StatusBadGateway)
				return
			}
			if !track(remote) {
				return
			}
			defer func() { remote.Close(); untrack(remote) }()
			client, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer func() { client.Close(); untrack(client) }()
			if _, err := buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				return
			}
			if err := buffer.Flush(); err != nil {
				return
			}
			done := make(chan struct{})
			go func() { io.Copy(remote, buffer); remote.Close(); close(done) }()
			io.Copy(client, remote)
			client.Close()
			remote.Close()
			<-done
			return
		}
		if !r.URL.IsAbs() || r.URL.Scheme != "http" {
			http.Error(w, "An absolute HTTP proxy request is required", http.StatusBadRequest)
			return
		}
		request := r.Clone(r.Context())
		request.RequestURI = ""
		upgrade := requestedUpgrade(request.Header)
		stripHopHeaders(request.Header)
		if upgrade != "" {
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", upgrade)
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			http.Error(w, "The configured proxy could not connect to this site", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusSwitchingProtocols {
			if upgrade == "" || !strings.EqualFold(upgrade, requestedUpgrade(response.Header)) {
				http.Error(w, "The configured proxy returned an unexpected protocol upgrade", http.StatusBadGateway)
				return
			}
			body, ok := response.Body.(io.ReadWriteCloser)
			if !ok {
				http.Error(w, "The configured proxy could not open the upgraded connection", http.StatusBadGateway)
				return
			}
			remote := &upgradedProxyStream{ReadWriteCloser: body}
			if !track(remote) {
				return
			}
			defer func() { remote.Close(); untrack(remote) }()
			client, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer func() { client.Close(); untrack(client) }()
			stripHopHeaders(response.Header)
			response.Header.Set("Connection", "Upgrade")
			response.Header.Set("Upgrade", upgrade)
			if _, err := buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\n"); err != nil {
				return
			}
			if err := response.Header.Write(buffer); err != nil {
				return
			}
			if _, err := buffer.WriteString("\r\n"); err != nil {
				return
			}
			if err := buffer.Flush(); err != nil {
				return
			}
			done := make(chan struct{})
			go func() { io.Copy(remote, buffer); remote.Close(); close(done) }()
			io.Copy(client, remote)
			client.Close()
			remote.Close()
			<-done
			return
		}
		stripHopHeaders(response.Header)
		for key, values := range response.Header {
			if !hopHeader(key) {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
		}
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	})
	var once sync.Once
	closeProxy := func() {
		once.Do(func() {
			mu.Lock()
			closed = true
			for conn := range connections {
				conn.Close()
			}
			mu.Unlock()
			server.Close()
			transport.CloseIdleConnections()
		})
	}
	stop := context.AfterFunc(ctx, closeProxy)
	go server.Serve(listener)
	return "http://" + listener.Addr().String(), func() { stop(); closeProxy() }, nil
}

type upgradedProxyStream struct{ io.ReadWriteCloser }

func requestedUpgrade(headers http.Header) string {
	found := false
	for _, value := range headers.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				found = true
			}
		}
	}
	if !found {
		return ""
	}
	protocol := strings.TrimSpace(headers.Get("Upgrade"))
	for _, char := range protocol {
		if char <= 32 || char >= 127 {
			return ""
		}
	}
	return protocol
}
func hopHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Transfer-Encoding", "Te", "Trailer", "Upgrade", "Proxy-Authenticate", "Proxy-Authorization":
		return true
	}
	return false
}

func stripHopHeaders(headers http.Header) {
	for _, value := range headers.Values("Connection") {
		for _, key := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(key))
		}
	}
	for key := range headers {
		if hopHeader(key) {
			headers.Del(key)
		}
	}
}
