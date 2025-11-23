//go:build windows

package secrets

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

// SecretManager is responsible for storing and retrieving secrets using the Windows Credential Manager
type SecretManager struct {
	service string
}

// NewSecretManager creates a new SecretManager instance
func NewSecretManager() *SecretManager {
	return &SecretManager{
		service: "Pangolin: pangolin-windows",
	}
}

// SaveSecret saves a secret value with the given key
// Returns true if successful, false otherwise
func (sm *SecretManager) SaveSecret(key, value string) bool {
	// Delete existing item if it exists (go-keyring doesn't have an update method)
	_ = sm.DeleteSecret(key)

	err := keyring.Set(sm.service, key, value)
	return err == nil
}

// GetSecret retrieves a secret value for the given key
// Returns the value if found, or an empty string and false if not found
func (sm *SecretManager) GetSecret(key string) (string, bool) {
	value, err := keyring.Get(sm.service, key)
	if err != nil {
		return "", false
	}
	return value, true
}

// DeleteSecret deletes a secret with the given key
// Returns true if successful or if the item was not found, false on error
func (sm *SecretManager) DeleteSecret(key string) bool {
	err := keyring.Delete(sm.service, key)
	// Consider both success and "not found" as success
	return err == nil || err == keyring.ErrNotFound
}

// GetOlmId retrieves the OLM ID for the given user ID
func (sm *SecretManager) GetOlmId(userId string) (string, bool) {
	return sm.GetSecret(sm.olmIdKey(userId))
}

// GetOlmSecret retrieves the OLM secret for the given user ID
func (sm *SecretManager) GetOlmSecret(userId string) (string, bool) {
	return sm.GetSecret(sm.olmSecretKey(userId))
}

// SaveOlmCredentials saves both OLM ID and secret for the given user ID
// Returns true if both were saved successfully
func (sm *SecretManager) SaveOlmCredentials(userId, olmId, secret string) bool {
	idSaved := sm.SaveSecret(sm.olmIdKey(userId), olmId)
	secretSaved := sm.SaveSecret(sm.olmSecretKey(userId), secret)
	return idSaved && secretSaved
}

// HasOlmCredentials checks if OLM credentials exist for the given user ID
func (sm *SecretManager) HasOlmCredentials(userId string) bool {
	_, hasId := sm.GetOlmId(userId)
	_, hasSecret := sm.GetOlmSecret(userId)
	return hasId && hasSecret
}

// DeleteOlmCredentials deletes both OLM ID and secret for the given user ID
// Returns true if both were deleted successfully (or didn't exist)
func (sm *SecretManager) DeleteOlmCredentials(userId string) bool {
	idDeleted := sm.DeleteSecret(sm.olmIdKey(userId))
	secretDeleted := sm.DeleteSecret(sm.olmSecretKey(userId))
	return idDeleted && secretDeleted
}

// olmIdKey returns the key for storing OLM ID
func (sm *SecretManager) olmIdKey(userId string) string {
	return fmt.Sprintf("olm-id-%s", userId)
}

// olmSecretKey returns the key for storing OLM secret
func (sm *SecretManager) olmSecretKey(userId string) string {
	return fmt.Sprintf("olm-secret-%s", userId)
}
