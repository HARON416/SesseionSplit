package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Uses an existing phone without resetting its installed apps or account data.
// Only the emulator started by this test is stopped on completion.
func TestAndroidInstagramGraphicsLive(t *testing.T) {
	name := os.Getenv("ISOLATE_ANDROID_INSTAGRAM_SMOKE_AVD")
	if name == "" {
		t.Skip("set ISOLATE_ANDROID_INSTAGRAM_SMOKE_AVD to a stopped AVD with Instagram installed")
	}
	service := newAndroidService()
	if binary := os.Getenv("ISOLATE_ANDROID_TEST_EMULATOR"); binary != "" {
		service.emulator = binary
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	state, err := service.GetAndroidWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, device := range state.Emulators {
		if device.Name == name {
			found = true
			if device.State != "stopped" {
				t.Fatal("stop the selected emulator before running this test")
			}
		}
	}
	if !found {
		t.Fatal("selected AVD was not found")
	}
	if err := service.StartAndroidEmulator(ctx, name); err != nil {
		t.Fatal(err)
	}
	defer service.ServiceShutdown()
	serial := ""
	bootDeadline := time.Now().Add(100 * time.Second)
	for time.Now().Before(bootDeadline) {
		state, err = service.GetAndroidWorkspace(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, device := range state.Emulators {
			if device.Name != name {
				continue
			}
			if device.StreamError != "" {
				t.Fatal(device.StreamError)
			}
			if device.State == "running" {
				boot, err := service.run(ctx, service.adb, "-s", device.Serial, "shell", "getprop", "sys.boot_completed")
				if err == nil && strings.TrimSpace(string(boot)) == "1" {
					serial = device.Serial
				}
			}
		}
		if serial != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if serial == "" {
		t.Fatal("emulator did not complete boot")
	}
	t.Log("Phone booted; opening Instagram with Vulkan disabled")
	if err := service.OpenAndroidInstagram(ctx, serial); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(45 * time.Second); time.Now().Before(deadline); {
		time.Sleep(3 * time.Second)
		service.mu.Lock()
		failure := service.failures[name]
		service.mu.Unlock()
		if failure != "" {
			t.Fatal(failure)
		}
		pid, err := service.run(ctx, service.adb, "-s", serial, "shell", "pidof", "com.instagram.android")
		if err != nil || strings.TrimSpace(string(pid)) == "" {
			t.Fatalf("Instagram or emulator stopped: %v", err)
		}
	}
	t.Log("Instagram and emulator remained running for 45 seconds")
}
