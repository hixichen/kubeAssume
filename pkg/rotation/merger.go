package rotation

import (
	"time"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

// Merger handles merging JWKS for rotation overlap periods.
type Merger struct {
	overlapPeriod time.Duration
}

// NewMerger creates a new Merger with the specified overlap period.
func NewMerger(overlapPeriod time.Duration) *Merger {
	return &Merger{
		overlapPeriod: overlapPeriod,
	}
}

// Merge combines current JWKS with stored keys that are still in overlap period.
func (m *Merger) Merge(current *bridge.JWKS, state *State, now time.Time) *bridge.JWKS {
	if current == nil && len(state.Keys) == 0 {
		return &bridge.JWKS{Keys: []bridge.JWK{}}
	}

	// Start with a new JWKS
	merged := &bridge.JWKS{
		Keys: make([]bridge.JWK, 0),
	}

	// Add all keys from current JWKS
	if current != nil {
		merged.Keys = append(merged.Keys, current.Keys...)
	}

	// Add keys from state that should still be kept
	for keyID, keyState := range state.Keys {
		// Skip if already in current (avoid duplicates)
		if current != nil && hasKey(current, keyID) {
			continue
		}

		// Keep if in overlap period
		if m.shouldKeepKey(keyState, now) {
			merged.Keys = append(merged.Keys, keyState.Key)
		}
	}

	return merged
}

// UpdateState updates the rotation state based on current JWKS
// Returns events for any detected changes.
func (m *Merger) UpdateState(current *bridge.JWKS, state *State, now time.Time) ([]Event, error) {
	var events []Event

	if state.Keys == nil {
		state.Keys = make(map[string]*KeyState)
	}

	// Track which keys are in current JWKS
	currentKeyIDs := make(map[string]bool)
	if current != nil {
		for _, key := range current.Keys {
			currentKeyIDs[key.Kid] = true
		}
	}

	// Process each key in current JWKS
	if current != nil {
		for _, key := range current.Keys {
			if existingState, exists := state.Keys[key.Kid]; exists {
				// Existing key: update LastSeen
				existingState.LastSeen = now
				state.Keys[key.Kid] = existingState
			} else {
				// New key: add to state
				state.Keys[key.Kid] = &KeyState{
					KeyID:     key.Kid,
					Key:       key,
					FirstSeen: now,
					LastSeen:  now,
				}
				events = append(events, Event{
					Type:      EventNewKey,
					KeyID:     key.Kid,
					Timestamp: now,
					Message:   "New key detected: " + key.Kid,
				})
			}
		}
	}

	// Mark keys not in current as for removal
	for keyID, keyState := range state.Keys {
		if !currentKeyIDs[keyID] && keyState.MarkedForRemoval == nil {
			keyState.MarkedForRemoval = &now
			state.Keys[keyID] = keyState
		}
	}

	// Update version and timestamp
	state.Version++
	state.LastUpdated = now

	return events, nil
}

// CleanupExpired removes keys that have exceeded the overlap period
// Returns events for removed keys.
func (m *Merger) CleanupExpired(state *State, now time.Time) []Event {
	var events []Event
	var keysToRemove []string

	for keyID, keyState := range state.Keys {
		if keyState.MarkedForRemoval != nil {
			// Check if overlap period has expired
			if now.Sub(*keyState.MarkedForRemoval) >= m.overlapPeriod {
				keysToRemove = append(keysToRemove, keyID)
				events = append(events, Event{
					Type:      EventKeyExpired,
					KeyID:     keyID,
					Timestamp: now,
					Message:   "Key expired and removed: " + keyID,
				})
			}
		}
	}

	// Remove expired keys
	for _, keyID := range keysToRemove {
		delete(state.Keys, keyID)
	}

	if len(keysToRemove) > 0 {
		state.Version++
		state.LastUpdated = now
	}

	return events
}

// GetPublishableJWKS returns the JWKS that should be published
// This includes all current keys plus keys in overlap period.
func (m *Merger) GetPublishableJWKS(state *State) *bridge.JWKS {
	jwks := &bridge.JWKS{
		Keys: make([]bridge.JWK, 0, len(state.Keys)),
	}

	for _, keyState := range state.Keys {
		jwks.Keys = append(jwks.Keys, keyState.Key)
	}

	return jwks
}

// shouldKeepKey determines if a key should still be published.
func (m *Merger) shouldKeepKey(keyState *KeyState, now time.Time) bool {
	// Key is current (not marked for removal)
	if keyState.MarkedForRemoval == nil {
		return true
	}
	// Key is in overlap period
	if now.Sub(*keyState.MarkedForRemoval) < m.overlapPeriod {
		return true
	}
	return false
}

// hasKey checks if a JWKS contains a key with the given ID.
func hasKey(jwks *bridge.JWKS, keyID string) bool {
	for _, key := range jwks.Keys {
		if key.Kid == keyID {
			return true
		}
	}
	return false
}
