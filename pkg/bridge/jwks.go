package bridge

import (
	"fmt"
)

// ValidateJWKS validates that a JWKS has at least one valid key.
func ValidateJWKS(jwks *JWKS) error {
	// Check keys array is not empty
	if jwks == nil {
		return fmt.Errorf("JWKS is nil")
	}

	if len(jwks.Keys) == 0 {
		return fmt.Errorf("JWKS contains no keys")
	}

	// Validate each key has required fields (kty, kid)
	for i, key := range jwks.Keys {
		if err := validateJWK(&key); err != nil {
			return fmt.Errorf("key at index %d is invalid: %w", i, err)
		}
	}

	return nil
}

// GetKeyIDs returns all key IDs from a JWKS.
func GetKeyIDs(jwks *JWKS) []string {
	if jwks == nil {
		return nil
	}

	keyIDs := make([]string, 0, len(jwks.Keys))
	for _, key := range jwks.Keys {
		if key.Kid != "" {
			keyIDs = append(keyIDs, key.Kid)
		}
	}
	return keyIDs
}

// FindKeyByID finds a key in the JWKS by its key ID.
func FindKeyByID(jwks *JWKS, keyID string) (*JWK, bool) {
	if jwks == nil {
		return nil, false
	}

	for i := range jwks.Keys {
		if jwks.Keys[i].Kid == keyID {
			return &jwks.Keys[i], true
		}
	}
	return nil, false
}

// MergeJWKS merges two JWKS, deduplicating by key ID
// Keys from jwks2 with the same key ID as jwks1 are ignored.
func MergeJWKS(jwks1, jwks2 *JWKS) *JWKS {
	if jwks1 == nil && jwks2 == nil {
		return &JWKS{Keys: []JWK{}}
	}
	if jwks1 == nil {
		return CloneJWKS(jwks2)
	}
	if jwks2 == nil {
		return CloneJWKS(jwks1)
	}

	// Create map of key IDs from jwks1
	keyIDs := make(map[string]bool)
	for _, key := range jwks1.Keys {
		keyIDs[key.Kid] = true
	}

	// Start with all keys from jwks1
	merged := CloneJWKS(jwks1)

	// Add keys from jwks2 that don't exist in jwks1
	for _, key := range jwks2.Keys {
		if !keyIDs[key.Kid] {
			merged.Keys = append(merged.Keys, key)
		}
	}

	return merged
}

// FilterKeys removes keys from JWKS that match the given key IDs.
func FilterKeys(jwks *JWKS, excludeKeyIDs []string) *JWKS {
	if jwks == nil {
		return nil
	}

	// Create set of excluded key IDs
	excludeSet := make(map[string]bool)
	for _, id := range excludeKeyIDs {
		excludeSet[id] = true
	}

	// Filter out matching keys
	filtered := &JWKS{
		Keys: make([]JWK, 0, len(jwks.Keys)),
	}

	for _, key := range jwks.Keys {
		if !excludeSet[key.Kid] {
			filtered.Keys = append(filtered.Keys, key)
		}
	}

	return filtered
}

// CloneJWKS creates a deep copy of a JWKS.
func CloneJWKS(jwks *JWKS) *JWKS {
	if jwks == nil {
		return nil
	}
	cloned := &JWKS{
		Keys: make([]JWK, len(jwks.Keys)),
	}
	copy(cloned.Keys, jwks.Keys)
	return cloned
}

// CompareJWKS compares two JWKS and returns added and removed key IDs.
func CompareJWKS(oldJWKS, newJWKS *JWKS) (added, removed []string) {
	if oldJWKS == nil && newJWKS == nil {
		return nil, nil
	}
	if oldJWKS == nil {
		return GetKeyIDs(newJWKS), nil
	}
	if newJWKS == nil {
		return nil, GetKeyIDs(oldJWKS)
	}

	oldKeyIDs := make(map[string]bool)
	for _, key := range oldJWKS.Keys {
		oldKeyIDs[key.Kid] = true
	}

	newKeyIDs := make(map[string]bool)
	for _, key := range newJWKS.Keys {
		newKeyIDs[key.Kid] = true
	}

	// Find keys in newJWKS not in oldJWKS (added)
	for keyID := range newKeyIDs {
		if !oldKeyIDs[keyID] {
			added = append(added, keyID)
		}
	}

	// Find keys in oldJWKS not in newJWKS (removed)
	for keyID := range oldKeyIDs {
		if !newKeyIDs[keyID] {
			removed = append(removed, keyID)
		}
	}

	return added, removed
}

// validateJWK validates a single JWK has required fields.
func validateJWK(jwk *JWK) error {
	if jwk.Kty == "" {
		return fmt.Errorf("key type (kty) is required")
	}
	if jwk.Kid == "" {
		return fmt.Errorf("key ID (kid) is required")
	}
	// RSA key validation
	if jwk.Kty == "RSA" {
		if jwk.N == "" || jwk.E == "" {
			return fmt.Errorf("RSA key requires n and e parameters")
		}
	}
	return nil
}
