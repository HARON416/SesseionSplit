package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Keep IANA timezones available on Windows without a Go installation.
)

type ProfileSettings struct {
	Proxy       ProxySettings `json:"proxy"`
	Engine      string        `json:"engine"`
	Language    string        `json:"language"`
	Timezone    string        `json:"timezone"`
	Width       int           `json:"width"`
	Height      int           `json:"height"`
	Permissions string        `json:"permissions"`
}
type ProxySettings struct {
	Mode         string `json:"mode"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	CredentialID string `json:"credentialId,omitempty"`
	HasPassword  bool   `json:"hasPassword"`
}
type SettingsUpdate struct {
	Settings       ProfileSettings `json:"settings"`
	Password       string          `json:"password"`
	PasswordAction string          `json:"passwordAction"`
}

var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`)
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

func validateProfileSettings(settings ProfileSettings) error {
	if err := validateProxySettings(settings.Proxy); err != nil {
		return err
	}
	if settings.Engine != "" && settings.Engine != "chrome" && settings.Engine != "brave" {
		return errors.New("Choose Chrome or Brave as the browser")
	}
	if settings.Language != "" && !languagePattern.MatchString(settings.Language) {
		return errors.New("Enter a language code such as en-US")
	}
	if settings.Timezone != "" {
		if _, err := time.LoadLocation(settings.Timezone); err != nil {
			return errors.New("Choose a valid timezone such as Africa/Nairobi")
		}
	}
	if (settings.Width != 0 || settings.Height != 0) && (settings.Width < 320 || settings.Width > 7680 || settings.Height < 200 || settings.Height > 4320) {
		return errors.New("Enter a window width from 320 to 7680 and height from 200 to 4320")
	}
	if settings.Permissions != "" && settings.Permissions != "ask" && settings.Permissions != "block" {
		return errors.New("Choose Ask or Block for site permissions")
	}
	return nil
}
func validateProxySettings(proxy ProxySettings) error {
	switch proxy.Mode {
	case "", "direct":
		return nil
	case "http", "https", "socks5":
	default:
		return errors.New("Choose a supported proxy type")
	}
	if proxy.Host == "" || strings.TrimSpace(proxy.Host) != proxy.Host || strings.ContainsAny(proxy.Host, "/@?#\\\t\r\n ") {
		return errors.New("Enter a proxy hostname or IP address without a URL or credentials")
	}
	host := proxy.Host
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if net.ParseIP(host) == nil && (!hostnamePattern.MatchString(host) || strings.Contains(host, "..")) {
		return errors.New("Enter a valid proxy hostname or IP address")
	}
	if proxy.Port < 1 || proxy.Port > 65535 {
		return errors.New("Enter a proxy port from 1 to 65535")
	}
	if len(proxy.Username) > 255 || strings.ContainsAny(proxy.Username, "\r\n\x00") {
		return errors.New("Enter a valid proxy username of at most 255 characters")
	}
	return nil
}

func (s *ProfileService) UpdateProfileSettings(id string, update SettingsUpdate) (BrowserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BrowserProfile{}, errors.New("SessionSplit is closing")
	}
	index := -1
	for i, p := range s.profiles {
		if p.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return BrowserProfile{}, errors.New("Profile was not found")
	}
	if s.browsers[id] != nil {
		return BrowserProfile{}, errors.New("Close this profile browser before changing settings")
	}
	if err := validateProfileSettings(update.Settings); err != nil {
		return BrowserProfile{}, err
	}
	if update.PasswordAction != "" && update.PasswordAction != "keep" && update.PasswordAction != "replace" && update.PasswordAction != "remove" {
		return BrowserProfile{}, errors.New("Choose whether to keep, replace, or remove the proxy password")
	}
	oldID := s.profiles[index].Settings.Proxy.CredentialID
	update.Settings.Proxy.CredentialID = oldID
	update.Settings.Proxy.HasPassword = oldID != ""
	if s.credentials == nil {
		s.credentials = newOSCredentialStore(s.root)
	}
	newID := ""
	switch update.PasswordAction {
	case "replace":
		if update.Password == "" {
			return BrowserProfile{}, errors.New("Enter a proxy password")
		}
		if len(update.Password) > 255 {
			return BrowserProfile{}, errors.New("Use a proxy password of at most 255 characters")
		}
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return BrowserProfile{}, errors.New("Could not prepare secure password storage; try again")
		}
		newID = hex.EncodeToString(token[:])
		if err := s.credentials.Set(newID, update.Password); err != nil {
			return BrowserProfile{}, errors.New("Could not save the proxy password in your system keyring; unlock it and try again")
		}
		update.Settings.Proxy.CredentialID = newID
		update.Settings.Proxy.HasPassword = true
	case "remove":
		update.Settings.Proxy.CredentialID = ""
		update.Settings.Proxy.HasPassword = false
	}
	next := append([]BrowserProfile{}, s.profiles...)
	next[index].Settings = update.Settings
	next[index].Browser = browserLabelForEngine(update.Settings.Engine)
	if err := s.saveProfiles(next); err != nil {
		if newID != "" {
			_ = s.credentials.Delete(newID)
		}
		return BrowserProfile{}, err
	}
	s.profiles = next
	if oldID != "" && oldID != update.Settings.Proxy.CredentialID {
		_ = s.credentials.Delete(oldID)
	}
	return next[index], nil
}
