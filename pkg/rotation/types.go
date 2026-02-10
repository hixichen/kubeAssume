// Package rotation provides key rotation detection and management
// for zero-downtime JWKS key rotation.
package rotation

import (
	"time"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

// EventType represents the type of rotation event.
type EventType string

const (
	// EventNewKey indicates a new key was detected.
	EventNewKey EventType = "NewKey"
	// EventKeyExpired indicates an old key was removed after overlap period.
	EventKeyExpired EventType = "KeyExpired"
	// EventNoChange indicates no rotation occurred.
	EventNoChange EventType = "NoChange"
)

// Event represents a key rotation event.
type Event struct {
	// Type is the type of rotation event
	Type EventType
	// KeyID is the key ID that was added or removed
	KeyID string
	// Timestamp is when the event occurred
	Timestamp time.Time
	// Message is a human-readable description
	Message string
}

// KeyState represents the state of a single key.
type KeyState struct {
	// KeyID is the unique identifier of the key
	KeyID string `json:"keyId"`
	// Key is the full JWK
	Key bridge.JWK `json:"key"`
	// FirstSeen is when the key was first observed
	FirstSeen time.Time `json:"firstSeen"`
	// LastSeen is when the key was last observed in the source JWKS
	LastSeen time.Time `json:"lastSeen"`
	// MarkedForRemoval is set when key disappears from source
	MarkedForRemoval *time.Time `json:"markedForRemoval,omitempty"`
}

// State represents the complete rotation state.
type State struct {
	// Keys is a map of key ID to key state
	Keys map[string]*KeyState `json:"keys"`
	// LastUpdated is when the state was last modified
	LastUpdated time.Time `json:"lastUpdated"`
	// Version is for optimistic locking
	Version int64 `json:"version"`
}

// Config holds configuration for the rotation manager.
type Config struct {
	// OverlapPeriod is how long to keep old keys after they disappear
	// Default: 24 hours
	OverlapPeriod time.Duration
	// Namespace is the K8s namespace for the state ConfigMap
	Namespace string
	// ConfigMapName is the name of the ConfigMap for storing state
	ConfigMapName string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		OverlapPeriod: 24 * time.Hour,
		Namespace:     "kubeassume-system",
		ConfigMapName: "kubeassume-rotation-state",
	}
}
