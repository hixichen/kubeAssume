package rotation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Store defines the interface for persisting rotation state.
type Store interface {
	// Load retrieves the current rotation state
	Load(ctx context.Context) (*State, error)
	// Save persists the rotation state
	Save(ctx context.Context, state *State) error
}

// ConfigMapStore implements Store using a Kubernetes ConfigMap.
type ConfigMapStore struct {
	client    kubernetes.Interface
	namespace string
	name      string
	logger    *slog.Logger
}

// NewConfigMapStore creates a new ConfigMapStore.
func NewConfigMapStore(client kubernetes.Interface, namespace, name string, logger *slog.Logger) *ConfigMapStore {
	return &ConfigMapStore{
		client:    client,
		namespace: namespace,
		name:      name,
		logger:    logger,
	}
}

// Load retrieves the rotation state from the ConfigMap.
func (s *ConfigMapStore) Load(ctx context.Context) (*State, error) {
	// Get ConfigMap from K8s
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		// If not found, return empty state
		if errors.IsNotFound(err) {
			s.logger.Debug("ConfigMap not found, returning empty state", "name", s.name)
			return emptyState(), nil
		}
		return nil, fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Unmarshal state from ConfigMap data
	stateData, ok := cm.Data["state"]
	if !ok {
		s.logger.Debug("No state data in ConfigMap, returning empty state")
		return emptyState(), nil
	}

	var state State
	if err := json.Unmarshal([]byte(stateData), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Ensure Keys map is initialized
	if state.Keys == nil {
		state.Keys = make(map[string]*KeyState)
	}

	s.logger.Debug("Loaded rotation state",
		"key_count", len(state.Keys),
		"version", state.Version,
	)

	return &state, nil
}

// Save persists the rotation state to the ConfigMap.
func (s *ConfigMapStore) Save(ctx context.Context, state *State) error {
	// Marshal state to JSON
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Get existing ConfigMap or create new one
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ConfigMap
			return s.createConfigMap(ctx, data)
		}
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Update existing ConfigMap
	return s.updateConfigMap(ctx, cm, data)
}

// createConfigMap creates a new ConfigMap for rotation state.
func (s *ConfigMapStore) createConfigMap(ctx context.Context, data []byte) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.name,
			Namespace: s.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "kube-iam-assume",
				"app.kubernetes.io/component": "rotation-state",
			},
		},
		Data: map[string]string{
			"state": string(data),
		},
	}

	_, err := s.client.CoreV1().ConfigMaps(s.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create ConfigMap: %w", err)
	}

	s.logger.Debug("Created rotation state ConfigMap", "name", s.name)
	return nil
}

// updateConfigMap updates an existing ConfigMap with new state.
func (s *ConfigMapStore) updateConfigMap(ctx context.Context, cm *corev1.ConfigMap, data []byte) error {
	cm.Data["state"] = string(data)

	_, err := s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	s.logger.Debug("Updated rotation state ConfigMap", "name", s.name)
	return nil
}

// emptyState returns an empty rotation state.
func emptyState() *State {
	return &State{
		Keys:    make(map[string]*KeyState),
		Version: 0,
	}
}
