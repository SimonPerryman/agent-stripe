package config

import "github.com/zalando/go-keyring"

// KeyringService is the service name used for keychain entries.
const KeyringService = "agent-stripe"

// SetSecret stores secret in the OS keychain under ref.
func SetSecret(ref, secret string) error {
	return keyring.Set(KeyringService, ref, secret)
}

// GetSecret retrieves the secret stored under ref.
func GetSecret(ref string) (string, error) {
	return keyring.Get(KeyringService, ref)
}

// DeleteSecret removes the secret stored under ref.
func DeleteSecret(ref string) error {
	return keyring.Delete(KeyringService, ref)
}
