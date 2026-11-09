package main

import (
	"os"
	"testing"
)

func TestOSCredentialStoreRoundTrip(t *testing.T) {
	if os.Getenv("ISOLATE_KEYRING_TESTS") != "1" {
		t.Skip("set ISOLATE_KEYRING_TESTS=1 to test the desktop credential store")
	}
	store := newOSCredentialStore(t.TempDir())
	const key = "temporary-integration-test"
	const value = "Isolate temporary credential test"
	if err := store.Set(key, value); err != nil {
		t.Fatal("system keyring could not save a temporary credential", err)
	}
	t.Cleanup(func() { _ = store.Delete(key) })
	got, err := store.Get(key)
	if err != nil || got != value {
		t.Fatal("system keyring could not read the temporary credential", err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatal("system keyring could not remove the temporary credential", err)
	}
	if _, err := store.Get(key); err == nil {
		t.Fatal("deleted credential remained available")
	}
}
