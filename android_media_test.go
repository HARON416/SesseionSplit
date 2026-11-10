package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestScrcpyPackets(t *testing.T) {
	payload := []byte{1, 2, 3}
	header := append(mp4u32(0x20000000, 123), mp4u32(uint32(len(payload)))...)
	flags, data, err := readScrcpyPacket(bytes.NewReader(append(header, payload...)))
	if err != nil || flags&(1<<61) == 0 || !bytes.Equal(data, payload) {
		t.Fatal("key frame packet lost", flags, data, err)
	}
	_, _, err = readScrcpyPacket(bytes.NewReader(append(mp4u32(0, 0), mp4u32(9*1024*1024)...)))
	if err == nil {
		t.Fatal("oversized packet accepted")
	}
	_, _, err = readScrcpyPacket(bytes.NewReader(header))
	if err != io.EOF {
		t.Fatal("truncated packet accepted", err)
	}
	flags, data, err = readScrcpyPacket(bytes.NewReader(mp4u32(0x80000000, 720, 1600)))
	if err != nil || flags>>63 == 0 || binary.BigEndian.Uint32(data[4:]) != 1600 {
		t.Fatal("v5 display metadata invalid", err)
	}
}
func TestMediaRejectsBadConfigurations(t *testing.T) {
	if _, _, err := mediaInit(true, []byte{1, 2}, 720, 1600); err == nil {
		t.Fatal("bad H.264 accepted")
	}
	if _, _, err := mediaInit(false, []byte{0xff, 0xff}, 0, 0); err == nil {
		t.Fatal("bad AAC accepted")
	}
	if _, err := avcSample([]byte{1, 2, 3}); err == nil {
		t.Fatal("bad sample accepted")
	}
}
func TestAndroidFragmentedMP4(t *testing.T) {
	if os.Getenv("ISOLATE_ANDROID_VIDEO_TESTS") != "1" {
		t.Skip("set ISOLATE_ANDROID_VIDEO_TESTS=1 to verify real encoded video and audio")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	run := func(name string, args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s failed: %s %v", name, out, err)
		}
		return out
	}
	raw := filepath.Join(root, "source.h264")
	run("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=360x800:rate=60", "-t", "1", "-c:v", "libx264", "-threads", "1", "-profile:v", "baseline", "-pix_fmt", "yuv420p", "-g", "60", "-f", "h264", raw)
	data, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Packets []struct {
			Pos   string `json:"pos"`
			Size  string `json:"size"`
			Flags string `json:"flags"`
		} `json:"packets"`
	}
	if err = json.Unmarshal(run("ffprobe", "-v", "error", "-show_packets", "-of", "json", raw), &probe); err != nil {
		t.Fatal(err)
	}
	var config []byte
	for _, nal := range annexBNALs(data) {
		if nal[0]&31 == 7 || nal[0]&31 == 8 {
			config = append(config, 0, 0, 0, 1)
			config = append(config, nal...)
		}
	}
	result, mime, err := mediaInit(true, config, 360, 800)
	if err != nil || !strings.Contains(mime, "avc1.") {
		t.Fatal(mime, err)
	}
	position := 0
	for i, packet := range probe.Packets {
		var size int
		size, _ = strconv.Atoi(packet.Size)
		sample, err := avcSample(data[position : position+size])
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, mediaFragment(uint32(i+1), uint64(i*1000000/60), 16667, strings.Contains(packet.Flags, "K"), sample)...)
		position += size
	}
	video := filepath.Join(root, "video.mp4")
	if err = os.WriteFile(video, result, 0600); err != nil {
		t.Fatal(err)
	}
	run("ffmpeg", "-v", "error", "-i", video, "-f", "null", "-")
	var videoProbe struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration string `json:"duration"`
		} `json:"streams"`
	}
	if err = json.Unmarshal(run("ffprobe", "-v", "error", "-show_streams", "-of", "json", video), &videoProbe); err != nil || len(videoProbe.Streams) != 1 || videoProbe.Streams[0].Width != 360 || videoProbe.Streams[0].Height != 800 {
		t.Fatal("invalid fragmented video", videoProbe, err)
	}
	if len(probe.Packets) != 60 {
		t.Fatalf("expected 60 video frames, got %d", len(probe.Packets))
	}
	aacPath := filepath.Join(root, "source.aac")
	run("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "1", "-ac", "2", "-c:a", "aac", "-f", "adts", aacPath)
	aac, err := os.ReadFile(aacPath)
	if err != nil {
		t.Fatal(err)
	}
	audio, _, err := mediaInit(false, []byte{0x11, 0x90}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for len(aac) >= 7 {
		size := int(aac[3]&3)<<11 | int(aac[4])<<3 | int(aac[5]>>5)
		if size < 7 || size > len(aac) {
			t.Fatal("invalid fixture ADTS")
		}
		audio = append(audio, mediaFragment(uint32(count+1), uint64(count)*1024*1000000/48000, 21333, true, aac[7:size])...)
		count++
		aac = aac[size:]
	}
	audioPath := filepath.Join(root, "audio.mp4")
	if err = os.WriteFile(audioPath, audio, 0600); err != nil {
		t.Fatal(err)
	}
	run("ffmpeg", "-v", "error", "-i", audioPath, "-f", "null", "-")
	if count < 46 {
		t.Fatal("too few AAC samples", count)
	}
	if os.Getenv("ISOLATE_ANDROID_BROWSER_TESTS") == "1" {
		for _, audioFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("audio_first_%t", audioFirst), func(t *testing.T) {
				testAndroidWebView(t, ctx, result, mime, audio, audioFirst)
			})
		}
		t.Run("audio_unavailable", func(t *testing.T) {
			testAndroidWebView(t, ctx, result, mime, nil, false)
		})
	}
}

func TestScrcpyLogRetainsBoundedTail(t *testing.T) {
	var log scrcpyLog
	_, _ = log.Write([]byte(strings.Repeat("x", 9000)))
	_, _ = log.Write([]byte("encoder failed"))
	if got := log.String(); len(got) != 8192 || !strings.HasSuffix(got, "encoder failed") {
		t.Fatalf("lost bounded diagnostics: length %d", len(got))
	}
}
