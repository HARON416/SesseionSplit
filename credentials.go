package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// credentialStore keeps secrets outside the profile registry. Tests inject a store.
type credentialStore interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}

type osCredentialStore struct{ service string }

func newOSCredentialStore(root string) credentialStore {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = filepath.Clean(root)
	}
	name := "Isolate"
	if strings.EqualFold(filepath.Base(absolute), "sessionsplit") {
		name = "SessionSplit"
	}
	return osCredentialStore{service: fmt.Sprintf("%s-%x", name, sha256.Sum256([]byte(absolute)))}
}
func (s osCredentialStore) Get(key string) (string, error) { return keyring.Get(s.service, key) }
func (s osCredentialStore) Set(key, value string) error    { return keyring.Set(s.service, key, value) }
func (s osCredentialStore) Delete(key string) error        { return keyring.Delete(s.service, key) }
