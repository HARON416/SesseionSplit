package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
)

// FingerprintIdentity is persistent input for the future native engine. Saving
// it does not enable fingerprint controls in ordinary Chrome or Brave.
type FingerprintIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	Seed          string `json:"seed"`
}

var fingerprintSeedPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Missing fields belong to legacy profiles. An explicitly saved invalid value,
// including null or {}, must never be mistaken for a missing identity.
func (identity *FingerprintIdentity) UnmarshalJSON(data []byte) error {
	type storedIdentity FingerprintIdentity
	var stored storedIdentity
	if err := json.Unmarshal(data, &stored); err != nil {
		return errors.New("Invalid saved fingerprint identity; your data has been preserved")
	}
	next := FingerprintIdentity(stored)
	if err := next.validate(); err != nil {
		return err
	}
	*identity = next
	return nil
}

func newFingerprintIdentity() (FingerprintIdentity, error) {
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return FingerprintIdentity{}, errors.New("Could not create the profile fingerprint identity")
	}
	return FingerprintIdentity{SchemaVersion: 1, Seed: hex.EncodeToString(seed[:])}, nil
}

func (identity FingerprintIdentity) validate() error {
	if identity.SchemaVersion != 1 || !fingerprintSeedPattern.MatchString(identity.Seed) {
		return errors.New("Invalid saved fingerprint identity; your data has been preserved")
	}
	return nil
}

// migrateFingerprintIdentities commits all new seeds atomically. Existing
// identities are never regenerated, including after settings changes.
func (s *ProfileService) migrateFingerprintIdentities() error {
	next := append([]BrowserProfile{}, s.profiles...)
	changed := false
	for i := range next {
		if next[i].Fingerprint != (FingerprintIdentity{}) {
			if err := next[i].Fingerprint.validate(); err != nil {
				return err
			}
			continue
		}
		identity, err := newFingerprintIdentity()
		if err != nil {
			return err
		}
		next[i].Fingerprint = identity
		changed = true
	}
	if changed {
		if err := s.saveProfiles(next); err != nil {
			return err
		}
		s.profiles = next
	}
	return nil
}
