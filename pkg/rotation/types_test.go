package rotation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 24*time.Hour, config.OverlapPeriod)
	assert.Equal(t, "kubeassume-system", config.Namespace)
	assert.Equal(t, "kubeassume-rotation-state", config.ConfigMapName)
}

func TestEventType_Constants(t *testing.T) {
	assert.Equal(t, EventType("NewKey"), EventNewKey)
	assert.Equal(t, EventType("KeyExpired"), EventKeyExpired)
	assert.Equal(t, EventType("NoChange"), EventNoChange)
}

func TestKeyState(t *testing.T) {
	now := time.Now()
	keyState := &KeyState{
		KeyID:     "test-key",
		Key:       bridge.JWK{Kid: "test-key", Kty: "RSA"},
		FirstSeen: now,
		LastSeen:  now,
	}

	assert.Equal(t, "test-key", keyState.KeyID)
	assert.Equal(t, "test-key", keyState.Key.Kid)
	assert.Equal(t, now, keyState.FirstSeen)
	assert.Equal(t, now, keyState.LastSeen)
	assert.Nil(t, keyState.MarkedForRemoval)
}

func TestKeyState_WithMarkedForRemoval(t *testing.T) {
	now := time.Now()
	removalTime := now.Add(-1 * time.Hour)
	keyState := &KeyState{
		KeyID:            "test-key",
		Key:              bridge.JWK{Kid: "test-key", Kty: "RSA"},
		FirstSeen:        now,
		LastSeen:         now,
		MarkedForRemoval: &removalTime,
	}

	assert.NotNil(t, keyState.MarkedForRemoval)
	assert.Equal(t, removalTime, *keyState.MarkedForRemoval)
}

func TestState(t *testing.T) {
	now := time.Now()
	state := &State{
		Keys: map[string]*KeyState{
			"key1": {
				KeyID:     "key1",
				Key:       bridge.JWK{Kid: "key1", Kty: "RSA"},
				FirstSeen: now,
				LastSeen:  now,
			},
		},
		LastUpdated: now,
		Version:     1,
	}

	assert.Len(t, state.Keys, 1)
	assert.Equal(t, now, state.LastUpdated)
	assert.Equal(t, int64(1), state.Version)
}

func TestState_EmptyKeys(t *testing.T) {
	now := time.Now()
	state := &State{
		Keys:        make(map[string]*KeyState),
		LastUpdated: now,
		Version:     0,
	}

	assert.Empty(t, state.Keys)
}

func TestEvent(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:      EventNewKey,
		KeyID:     "test-key",
		Timestamp: now,
		Message:   "New key detected: test-key",
	}

	assert.Equal(t, EventNewKey, event.Type)
	assert.Equal(t, "test-key", event.KeyID)
	assert.Equal(t, now, event.Timestamp)
	assert.Equal(t, "New key detected: test-key", event.Message)
}

func TestEvent_Events(t *testing.T) {
	now := time.Now()

	events := []Event{
		{
			Type:      EventNewKey,
			KeyID:     "key1",
			Timestamp: now,
			Message:   "New key detected",
		},
		{
			Type:      EventKeyExpired,
			KeyID:     "key2",
			Timestamp: now,
			Message:   "Key expired",
		},
		{
			Type:      EventNoChange,
			KeyID:     "",
			Timestamp: now,
			Message:   "No change",
		},
	}

	assert.Len(t, events, 3)
	assert.Equal(t, EventNewKey, events[0].Type)
	assert.Equal(t, EventKeyExpired, events[1].Type)
	assert.Equal(t, EventNoChange, events[2].Type)
}

func TestConfig_CustomValues(t *testing.T) {
	config := Config{
		OverlapPeriod: 1 * time.Hour,
		Namespace:     "custom-ns",
		ConfigMapName: "custom-state",
	}

	assert.Equal(t, 1*time.Hour, config.OverlapPeriod)
	assert.Equal(t, "custom-ns", config.Namespace)
	assert.Equal(t, "custom-state", config.ConfigMapName)
}
