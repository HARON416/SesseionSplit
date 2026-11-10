package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const scrcpyVersion = "5.0.1"
const scrcpySHA256 = "764eb6f79811d5211fe9df341120882ba9994c7a61b897d7bf3fb662e53bc536"

func readScrcpyPacket(reader io.Reader) (flags uint64, data []byte, err error) {
	var header [12]byte
	if _, err = io.ReadFull(reader, header[:]); err != nil {
		return
	}
	flags = binary.BigEndian.Uint64(header[:8])
	if flags>>63 != 0 {
		return flags, header[4:], nil
	} // v5 session metadata: flags, width, height
	size := binary.BigEndian.Uint32(header[8:])
	if size == 0 || size > 8*1024*1024 {
		return 0, nil, errors.New("Invalid scrcpy packet length")
	}
	data = make([]byte, size)
	_, err = io.ReadFull(reader, data)
	return
}

func (s *AndroidService) serveScreen(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	s.mu.Lock()
	var stream *androidStream
	if r.Method == http.MethodGet && len(parts) == 2 && subtle.ConstantTimeCompare([]byte(parts[0]), []byte(s.token)) == 1 {
		stream = s.streams[parts[1]]
	}
	adb, server := s.adb, s.serverPath
	s.mu.Unlock()
	if stream == nil || stream.ctx.Err() != nil || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("session")), []byte(stream.key)) != 1 {
		http.NotFound(w, r)
		return
	}
	// The unguessable capability URL is required even for native WebViews, whose
	// origins differ by platform. Do not expose an unauthenticated websocket.
	w.Header().Set("Cache-Control", "no-store")
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	ctx, cancel := context.WithCancel(stream.ctx)
	defer cancel()
	stream.mu.Lock()
	if stream.sessionCancel != nil {
		stream.sessionCancel()
	}
	stream.sessionCancel = cancel
	stream.err = ""
	stream.audio = "Connecting audio…"
	stream.mu.Unlock()
	// Read control frames, detect a closed WebView, and stop the device session.
	readCtx := connection.CloseRead(ctx)
	err = stream.captureScrcpy(readCtx, connection, adb, server, parts[1])
	if err != nil && ctx.Err() == nil && readCtx.Err() == nil {
		stream.mu.Lock()
		stream.err = err.Error()
		stream.mu.Unlock()
		payload, _ := json.Marshal(map[string]string{"error": "Android screen disconnected: " + err.Error()})
		writeCtx, done := context.WithTimeout(ctx, time.Second)
		_ = connection.Write(writeCtx, websocket.MessageText, payload)
		done()
	}
}

func (stream *androidStream) captureScrcpy(parent context.Context, ws *websocket.Conn, adb, server, serial string) (result error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		task, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		return androidCommandOutput(task, adb, append([]string{"-s", serial}, args...)...)
	}
	identity, err := run("emu", "avd", "name")
	if err != nil || len(strings.Fields(string(identity))) == 0 || strings.Fields(string(identity))[0] != stream.avdName {
		return errors.New("Emulator identity changed; reconnect its screen")
	}
	var random [4]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	scid := fmt.Sprintf("%08x", binary.BigEndian.Uint32(random[:])&0x7fffffff)
	remote := "/data/local/tmp/isolate-scrcpy-" + scid + ".jar"
	if _, err = run("push", server, remote); err != nil {
		return errors.New("Could not send the scrcpy server to Android")
	}
	defer func() {
		clean, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = androidCommandOutput(clean, adb, "-s", serial, "shell", "rm", "-f", remote)
	}()
	portData, err := run("forward", "tcp:0", "localabstract:scrcpy_"+scid)
	if err != nil {
		return errors.New("Could not forward the Android streaming socket")
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(portData)))
	if err != nil || port < 1 || port > 65535 {
		return errors.New("Invalid ADB forwarding port")
	}
	defer func() {
		clean, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = androidCommandOutput(clean, adb, "-s", serial, "forward", "--remove", "tcp:"+strconv.Itoa(port))
	}()
	command := exec.CommandContext(ctx, adb, "-s", serial, "shell", "CLASSPATH="+remote, "app_process", "/", "com.genymobile.scrcpy.Server", scrcpyVersion, "scid="+scid, "tunnel_forward=true", "control=false", "cleanup=false", "send_device_meta=false", "video_codec=h264", "audio_codec=aac", "max_size=0", "max_fps=30", "video_bit_rate=16000000", "audio_bit_rate=192000", "video_codec_options=profile=1,i-frame-interval=1,repeat-previous-frame-after:long=100000", "log_level=warn")
	var logs scrcpyLog
	command.Stdout = &logs
	command.Stderr = &logs
	hideAndroidCommand(command)
	if err = command.Start(); err != nil {
		return errors.New("Could not start Android streaming")
	}
	defer func() {
		cancel()
		_ = command.Wait()
		if result != nil && parent.Err() == nil {
			if detail := logs.String(); detail != "" {
				result = fmt.Errorf("%w\nscrcpy: %s", result, detail)
			}
		}
	}()
	// ADB accepts TCP before the Android localabstract listener is ready. The
	// dummy byte is the readiness handshake; failed attempts are retried.
	var video net.Conn
	deadline := time.Now().Add(15 * time.Second)
	for ctx.Err() == nil && time.Now().Before(deadline) {
		video, err = (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			_ = video.SetReadDeadline(time.Now().Add(time.Second))
			var dummy [1]byte
			_, err = io.ReadFull(video, dummy[:])
			if err == nil && dummy[0] == 0 {
				_ = video.SetReadDeadline(time.Time{})
				break
			}
			_ = video.Close()
			video = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if video == nil {
		return errors.New("Android streaming server did not become ready")
	}
	defer video.Close()
	audio, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer audio.Close()
	go func() { <-ctx.Done(); _ = video.Close(); _ = audio.Close() }()
	var writeMu sync.Mutex
	write := func(kind websocket.MessageType, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		task, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		return ws.Write(task, kind, data)
	}
	var clockMu sync.Mutex
	var origin uint64
	clockSet := false
	normalize := func(pts uint64) uint64 {
		clockMu.Lock()
		defer clockMu.Unlock()
		if !clockSet {
			origin = pts
			clockSet = true
		}
		if pts < origin {
			return 0
		}
		return pts - origin
	}
	disableAudio := func(reason string) error {
		stream.mu.Lock()
		stream.audio = reason
		stream.mu.Unlock()
		return write(websocket.MessageText, []byte(`{"track":1,"disabled":true}`))
	}
	errorsCh := make(chan error, 2)
	for index, conn := range []net.Conn{video, audio} {
		go func(track byte, conn net.Conn) {
			var codec [4]byte
			if _, err := io.ReadFull(conn, codec[:]); err != nil {
				if track == 1 && ctx.Err() == nil {
					if writeErr := disableAudio("Audio unavailable: " + err.Error()); writeErr != nil {
						errorsCh <- writeErr
					}
				} else {
					errorsCh <- fmt.Errorf("Android %s codec header: %w", trackName(track), err)
				}
				return
			}
			if track == 1 && binary.BigEndian.Uint32(codec[:]) <= 1 {
				stream.mu.Lock()
				stream.audio = "Audio unavailable on this Android device"
				stream.mu.Unlock()
				if err := write(websocket.MessageText, []byte(`{"track":1,"disabled":true}`)); err != nil {
					errorsCh <- err
				}
				return
			}
			expected := "h264"
			if track == 1 {
				expected = "\x00aac"
			}
			if string(codec[:]) != expected {
				errorsCh <- errors.New("Unexpected Android streaming codec")
				return
			}
			width, height := stream.width, stream.height
			sequence := uint32(0)
			configured := false
			var previous []byte
			var previousPTS uint64
			var previousKey bool
			for {
				flags, data, err := readScrcpyPacket(conn)
				if err != nil {
					if track == 1 && ctx.Err() == nil {
						if writeErr := disableAudio("Audio disconnected; video is still playing: " + err.Error()); writeErr != nil {
							errorsCh <- writeErr
						}
						return
					}
					errorsCh <- fmt.Errorf("Android %s packets: %w", trackName(track), err)
					return
				}
				if flags>>63 != 0 {
					width = int(binary.BigEndian.Uint32(data[:4]))
					height = int(binary.BigEndian.Uint32(data[4:]))
					continue
				}
				if flags&(1<<62) != 0 {
					if configured {
						errorsCh <- errors.New("Android display configuration changed; retry the screen")
						return
					}
					init, mime, err := mediaInit(track == 0, data, width, height)
					if err != nil {
						errorsCh <- err
						return
					}
					// JSON is built from fixed MIME names and hex codec bytes only.
					if err = write(websocket.MessageText, []byte(fmt.Sprintf(`{"track":%d,"mime":%q}`, track, mime))); err == nil {
						err = write(websocket.MessageBinary, append([]byte{track}, init...))
					}
					if err != nil {
						errorsCh <- err
						return
					}
					configured = true
					if track == 1 {
						stream.mu.Lock()
						stream.audio = "Audio available"
						stream.mu.Unlock()
					}
					continue
				}
				if !configured {
					errorsCh <- errors.New("Android codec configuration was not received")
					return
				}
				pts := normalize(flags & ((1 << 61) - 1))
				key := flags&(1<<61) != 0
				if track == 0 {
					data, err = avcSample(data)
					if err != nil {
						errorsCh <- err
						return
					}
				}
				// Keep one sample to calculate its true duration. The encoder repeats idle
				// frames, so a still screen also advances the media timeline.
				if previous != nil && pts > previousPTS {
					duration := pts - previousPTS
					if duration > 1000000 {
						duration = 1000000
					}
					sequence++
					fragment := mediaFragment(sequence, previousPTS, uint32(duration), track == 1 || previousKey, previous)
					if err = write(websocket.MessageBinary, append([]byte{track}, fragment...)); err != nil {
						errorsCh <- err
						return
					}
				}
				previous, previousPTS, previousKey = data, pts, key
			}
		}(byte(index), conn)
	}
	select {
	case err := <-errorsCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Keep the latest server diagnostics without allowing noisy encoders to grow memory.
type scrcpyLog struct {
	mu   sync.Mutex
	data []byte
}

func (l *scrcpyLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	const limit = 8192
	if len(p) >= limit {
		l.data = append(l.data[:0], p[len(p)-limit:]...)
	} else {
		if excess := len(l.data) + len(p) - limit; excess > 0 {
			l.data = l.data[excess:]
		}
		l.data = append(l.data, p...)
	}
	return n, nil
}
func (l *scrcpyLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(string(l.data))
}
func trackName(track byte) string {
	if track == 0 {
		return "video"
	}
	return "audio"
}
