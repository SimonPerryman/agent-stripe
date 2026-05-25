package config

import (
	"errors"
	"testing"
)

// memKeyring is an in-memory Keyring used by tests. Keyed by service+ref.
type memKeyring struct {
	store map[string]string
}

func newMemKeyring() *memKeyring { return &memKeyring{store: map[string]string{}} }

func (m *memKeyring) Set(service, key, secret string) error {
	m.store[service+"/"+key] = secret
	return nil
}

func (m *memKeyring) Get(service, key string) (string, error) {
	v, ok := m.store[service+"/"+key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (m *memKeyring) Delete(service, key string) error {
	delete(m.store, service+"/"+key)
	return nil
}

func TestKeyringRoundTrip(t *testing.T) {
	m := newMemKeyring()
	restore := SetKeyring(m)
	defer restore()

	if err := SetSecret("ref-1", "sk_test_123"); err != nil {
		t.Fatal(err)
	}
	got, err := GetSecret("ref-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk_test_123" {
		t.Errorf("expected sk_test_123, got %q", got)
	}
	if err := DeleteSecret("ref-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetSecret("ref-1"); err == nil {
		t.Error("expected missing key error after delete")
	}
}

func TestSetKeyringRestores(t *testing.T) {
	prev := defaultKeyring
	restore := SetKeyring(newMemKeyring())
	if defaultKeyring == prev {
		t.Fatal("expected keyring to be swapped")
	}
	restore()
	if defaultKeyring != prev {
		t.Error("expected keyring restored to prior value")
	}
}
