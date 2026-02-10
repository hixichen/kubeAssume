package rotation

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

// mockStore is a mock implementation of the Store interface for testing.
type mockStore struct {
	state *State
	err   error
}

func (m *mockStore) Load(ctx context.Context) (*State, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.state == nil {
		return emptyState(), nil
	}
	return m.state, nil
}

func (m *mockStore) Save(ctx context.Context, state *State) error {
	if m.err != nil {
		return m.err
	}
	m.state = state
	return nil
}

func TestNewManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	store := &mockStore{state: emptyState()}
	config := DefaultConfig()

	manager := NewManager(store, config, logger)
	require.NotNil(t, manager)
	assert.NotNil(t, manager.store)
	assert.NotNil(t, manager.merger)
	assert.Equal(t, config, manager.config)
	assert.NotNil(t, manager.logger)
}

func TestRotationManager_ProcessJWKS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	store := &mockStore{state: emptyState()}
	config := Config{
		OverlapPeriod: 24 * time.Hour,
	}

	manager := NewManager(store, config, logger)
	ctx := context.Background()

	// Create a test JWKS with one key
	jwks := &bridge.JWKS{
		Keys: []bridge.JWK{
			{Kid: "key1", Kty: "RSA"},
		},
	}

	result, events, err := manager.ProcessJWKS(ctx, jwks)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, events, 1)
	assert.Equal(t, EventNewKey, events[0].Type)
	assert.Equal(t, "key1", events[0].KeyID)
}

func TestRotationManager_ProcessJWKS_StoreError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	store := &mockStore{err: errors.New("store error")}
	config := DefaultConfig()

	manager := NewManager(store, config, logger)
	ctx := context.Background()

	jwks := &bridge.JWKS{
		Keys: []bridge.JWK{
			{Kid: "key1", Kty: "RSA"},
		},
	}

	_, _, err := manager.ProcessJWKS(ctx, jwks)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store error")
}

func TestRotationManager_GetPublishableJWKS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	now := time.Now()
	store := &mockStore{
		state: &State{
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
		},
	}
	config := DefaultConfig()

	manager := NewManager(store, config, logger)
	ctx := context.Background()

	result, err := manager.GetPublishableJWKS(ctx)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Keys, 1)
	assert.Equal(t, "key1", result.Keys[0].Kid)
}

func TestRotationManager_CleanupExpiredKeys(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	now := time.Now()
	pastTime := now.Add(-25 * time.Hour) // Past the default 24h overlap

	store := &mockStore{
		state: &State{
			Keys: map[string]*KeyState{
				"key1": {
					KeyID:            "key1",
					Key:              bridge.JWK{Kid: "key1", Kty: "RSA"},
					FirstSeen:        pastTime,
					LastSeen:         pastTime,
					MarkedForRemoval: &pastTime,
				},
			},
			LastUpdated: now,
			Version:     1,
		},
	}
	config := Config{
		OverlapPeriod: 24 * time.Hour,
	}

	manager := NewManager(store, config, logger)
	manager.SetTimeFunc(func() time.Time { return now })
	ctx := context.Background()

	events, err := manager.CleanupExpiredKeys(ctx)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventKeyExpired, events[0].Type)
	assert.Equal(t, "key1", events[0].KeyID)
}

func TestRotationManager_CleanupExpiredKeys_NoExpired(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	now := time.Now()
	recentTime := now.Add(-1 * time.Hour) // Within the overlap period

	store := &mockStore{
		state: &State{
			Keys: map[string]*KeyState{
				"key1": {
					KeyID:            "key1",
					Key:              bridge.JWK{Kid: "key1", Kty: "RSA"},
					FirstSeen:        recentTime,
					LastSeen:         recentTime,
					MarkedForRemoval: &recentTime,
				},
			},
			LastUpdated: now,
			Version:     1,
		},
	}
	config := Config{
		OverlapPeriod: 24 * time.Hour,
	}

	manager := NewManager(store, config, logger)
	manager.SetTimeFunc(func() time.Time { return now })
	ctx := context.Background()

	events, err := manager.CleanupExpiredKeys(ctx)
	require.NoError(t, err)
	assert.Len(t, events, 0)
}

func TestRotationManager_GetState(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	now := time.Now()
	store := &mockStore{
		state: &State{
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
		},
	}
	config := DefaultConfig()

	manager := NewManager(store, config, logger)
	ctx := context.Background()

	state, err := manager.GetState(ctx)
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Len(t, state.Keys, 1)
}

func TestCreateNewKeyEvent(t *testing.T) {
	now := time.Now()
	event := createNewKeyEvent("key123", now)

	assert.Equal(t, EventNewKey, event.Type)
	assert.Equal(t, "key123", event.KeyID)
	assert.Equal(t, now, event.Timestamp)
	assert.Contains(t, event.Message, "key123")
}

func TestCreateKeyExpiredEvent(t *testing.T) {
	now := time.Now()
	event := createKeyExpiredEvent("key456", now)

	assert.Equal(t, EventKeyExpired, event.Type)
	assert.Equal(t, "key456", event.KeyID)
	assert.Equal(t, now, event.Timestamp)
	assert.Contains(t, event.Message, "key456")
}
