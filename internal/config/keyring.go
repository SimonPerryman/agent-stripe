package config

import "github.com/zalando/go-keyring"

// KeyringService is the service name used for keychain entries.
const KeyringService = "agent-stripe"

// Keyring abstracts the OS keychain so tests can substitute an in-memory
// implementation. The default backend forwards to zalando/go-keyring.
type Keyring interface {
	Set(service, key, secret string) error
	Get(service, key string) (string, error)
	Delete(service, key string) error
}

type osKeyring struct{}

func (osKeyring) Set(service, key, secret string) error { return keyring.Set(service, key, secret) }
func (osKeyring) Get(service, key string) (string, error) {
	return keyring.Get(service, key)
}
func (osKeyring) Delete(service, key string) error { return keyring.Delete(service, key) }

var defaultKeyring Keyring = osKeyring{}

// SetKeyring swaps the active backend (used by tests). The returned function
// restores the previous backend.
func SetKeyring(k Keyring) func() {
	prev := defaultKeyring
	defaultKeyring = k
	return func() { defaultKeyring = prev }
}

// SetSecret stores secret in the OS keychain under ref.
func SetSecret(ref, secret string) error {
	return defaultKeyring.Set(KeyringService, ref, secret)
}

// GetSecret retrieves the secret stored under ref.
func GetSecret(ref string) (string, error) {
	return defaultKeyring.Get(KeyringService, ref)
}

// DeleteSecret removes the secret stored under ref.
func DeleteSecret(ref string) error {
	return defaultKeyring.Delete(KeyringService, ref)
}
