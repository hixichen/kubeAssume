package rotation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

// Manager defines the interface for rotation management.
type Manager interface {
	// ProcessJWKS processes a new JWKS and returns the merged result
	// Also returns any rotation events that occurred
	ProcessJWKS(ctx context.Context, current *bridge.JWKS) (*bridge.JWKS, []Event, error)

	// GetPublishableJWKS returns the current JWKS that should be published
	GetPublishableJWKS(ctx context.Context) (*bridge.JWKS, error)

	// CleanupExpiredKeys removes keys that have exceeded the overlap period
	CleanupExpiredKeys(ctx context.Context) ([]Event, error)

	// GetState returns the current rotation state
	GetState(ctx context.Context) (*State, error)
}

// RotationManager implements Manager.
type RotationManager struct {
	store   Store
	merger  *Merger
	config  Config
	logger  *slog.Logger
	nowFunc func() time.Time // For testing
}

// NewManager creates a new RotationManager.
func NewManager(store Store, cfg Config, logger *slog.Logger) *RotationManager {
	return &RotationManager{
		store:   store,
		merger:  NewMerger(cfg.OverlapPeriod),
		config:  cfg,
		logger:  logger,
		nowFunc: time.Now,
	}
}

// ProcessJWKS processes a new JWKS from the API server.
func (m *RotationManager) ProcessJWKS(ctx context.Context, current *bridge.JWKS) (*bridge.JWKS, []Event, error) {
	now := m.nowFunc()

	// Load current state from store
	state, err := m.store.Load(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load rotation state: %w", err)
	}

	var allEvents []Event

	// Update state with current JWKS (detect new/missing keys)
	events, err := m.merger.UpdateState(current, state, now)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update rotation state: %w", err)
	}
	allEvents = append(allEvents, events...)

	// Cleanup expired keys
	expiredEvents := m.merger.CleanupExpired(state, now)
	allEvents = append(allEvents, expiredEvents...)

	// Save updated state
	if err := m.store.Save(ctx, state); err != nil {
		return nil, nil, fmt.Errorf("failed to save rotation state: %w", err)
	}

	// Merge JWKS for publishing
	merged := m.merger.Merge(current, state, now)

	// Log events
	for _, event := range allEvents {
		m.logEvent(event)
	}

	return merged, allEvents, nil
}

// GetPublishableJWKS returns the current JWKS that should be published.
func (m *RotationManager) GetPublishableJWKS(ctx context.Context) (*bridge.JWKS, error) {
	// Load state from store
	state, err := m.store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load rotation state: %w", err)
	}

	// Build JWKS from state
	return m.merger.GetPublishableJWKS(state), nil
}

// CleanupExpiredKeys removes keys that have exceeded the overlap period.
func (m *RotationManager) CleanupExpiredKeys(ctx context.Context) ([]Event, error) {
	now := m.nowFunc()

	// Load state from store
	state, err := m.store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load rotation state: %w", err)
	}

	// Run cleanup
	events := m.merger.CleanupExpired(state, now)

	// Save state if changed
	if len(events) > 0 {
		if err := m.store.Save(ctx, state); err != nil {
			return nil, fmt.Errorf("failed to save rotation state: %w", err)
		}
	}

	// Log events
	for _, event := range events {
		m.logEvent(event)
	}

	return events, nil
}

// GetState returns the current rotation state.
func (m *RotationManager) GetState(ctx context.Context) (*State, error) {
	return m.store.Load(ctx)
}

// SetTimeFunc sets the time function (for testing).
func (m *RotationManager) SetTimeFunc(f func() time.Time) {
	m.nowFunc = f
}

// logEvent logs a rotation event.
func (m *RotationManager) logEvent(event Event) {
	m.logger.Info("rotation event",
		"type", event.Type,
		"keyId", event.KeyID,
		"message", event.Message,
	)
}

// createNewKeyEvent creates an event for a new key detection.
func createNewKeyEvent(keyID string, now time.Time) Event {
	return Event{
		Type:      EventNewKey,
		KeyID:     keyID,
		Timestamp: now,
		Message:   fmt.Sprintf("New key detected: %s", keyID),
	}
}

// createKeyExpiredEvent creates an event for key expiration.
func createKeyExpiredEvent(keyID string, now time.Time) Event {
	return Event{
		Type:      EventKeyExpired,
		KeyID:     keyID,
		Timestamp: now,
		Message:   fmt.Sprintf("Key expired and removed: %s", keyID),
	}
}
