package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type browserSpec struct {
	id       string
	name     string
	commands []string
	windows  []string
	mac      string
	linux    []string
}

var browserCatalog = []browserSpec{
	{id: "chrome", name: "Chrome / Chromium"},
	{id: "chromium", name: "Chromium", commands: []string{"chromium", "chromium-browser"}, windows: []string{"Chromium/Application/chrome.exe"}, mac: "Chromium.app/Contents/MacOS/Chromium", linux: []string{"/snap/bin/chromium"}},
	{id: "brave", name: "Brave", commands: []string{"brave-browser", "brave-browser-stable", "brave"}, windows: []string{"BraveSoftware/Brave-Browser/Application/brave.exe"}, mac: "Brave Browser.app/Contents/MacOS/Brave Browser", linux: []string{"/opt/brave.com/brave/brave-browser", "/snap/bin/brave"}},
	{id: "edge", name: "Microsoft Edge", commands: []string{"microsoft-edge", "microsoft-edge-stable", "msedge"}, windows: []string{"Microsoft/Edge/Application/msedge.exe"}, mac: "Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
	{id: "vivaldi", name: "Vivaldi", commands: []string{"vivaldi", "vivaldi-stable"}, windows: []string{"Vivaldi/Application/vivaldi.exe"}, mac: "Vivaldi.app/Contents/MacOS/Vivaldi"},
	{id: "opera", name: "Opera", commands: []string{"opera", "opera-stable"}, windows: []string{"Programs/Opera/opera.exe", "Opera/opera.exe", "Opera/launcher.exe"}, mac: "Opera.app/Contents/MacOS/Opera", linux: []string{"/snap/bin/opera"}},
	{id: "opera-gx", name: "Opera GX", commands: []string{"opera-gx"}, windows: []string{"Programs/Opera GX/opera.exe", "Opera GX/opera.exe", "Opera GX/launcher.exe"}, mac: "Opera GX.app/Contents/MacOS/Opera GX"},
	{id: "ungoogled-chromium", name: "Ungoogled Chromium", commands: []string{"ungoogled-chromium"}, windows: []string{"Ungoogled Chromium/Application/chrome.exe"}, mac: "Ungoogled Chromium.app/Contents/MacOS/Chromium"},
	{id: "thorium", name: "Thorium", commands: []string{"thorium-browser", "thorium"}, windows: []string{"Thorium/Application/thorium.exe", "Thorium/Application/chrome.exe"}, mac: "Thorium.app/Contents/MacOS/Thorium"},
	{id: "firefox", name: "Firefox", commands: []string{"firefox", "firefox-esr"}, windows: []string{"Mozilla Firefox/firefox.exe"}, mac: "Firefox.app/Contents/MacOS/firefox", linux: []string{"/opt/firefox/firefox", "/snap/bin/firefox"}},
	{id: "custom", name: "Other Chromium-based browser"},
}

func browserDefinition(id string) (browserSpec, bool) {
	if id == "" {
		id = "chrome"
	}
	for _, browser := range browserCatalog {
		if browser.id == id {
			return browser, true
		}
	}
	return browserSpec{}, false
}

func browserCandidates(id, platform string, getenv func(string) string, home string) []string {
	if id == "chrome" || id == "" {
		return chromeCandidates(platform, getenv)
	}
	browser, found := browserDefinition(id)
	if !found || id == "custom" {
		return nil
	}
	paths := []string{}
	if platform == "windows" {
		for _, relative := range browser.windows {
			for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
				if root := getenv(key); root != "" {
					paths = append(paths, filepath.Join(root, filepath.FromSlash(relative)))
				}
			}
		}
		return paths
	}
	paths = append(paths, browser.commands...)
	if platform == "darwin" && browser.mac != "" {
		paths = append(paths, filepath.Join("/Applications", browser.mac))
		if home != "" {
			paths = append(paths, filepath.Join(home, "Applications", browser.mac))
		}
	} else if platform == "linux" {
		paths = append(paths, browser.linux...)
	}
	return paths
}

func findEnginePath(id string) string {
	home, _ := os.UserHomeDir()
	for _, candidate := range browserCandidates(id, runtime.GOOS, os.Getenv, home) {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

func validateBrowserSelection(settings ProfileSettings) error {
	if _, found := browserDefinition(settings.Engine); !found {
		return errors.New("Choose a supported browser")
	}
	if settings.Engine != "custom" {
		return nil
	}
	path := settings.BrowserPath
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return errors.New("Enter the full path to a Chromium-based browser executable")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0111 == 0) {
		return errors.New("The selected browser executable is missing or cannot be run")
	}
	return nil
}
