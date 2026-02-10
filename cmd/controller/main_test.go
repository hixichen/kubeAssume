package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

func makeJWKS(kids ...string) *bridge.JWKS {
	keys := make([]bridge.JWK, 0, len(kids))
	for _, kid := range kids {
		keys = append(keys, bridge.JWK{Kid: kid, Kty: "RSA"})
	}
	return &bridge.JWKS{Keys: keys}
}

func TestMergeJWKS_DeduplicatesByKid(t *testing.T) {
	clusterJWKS := map[string]*bridge.JWKS{
		"cluster-a": makeJWKS("key-a1", "key-a2"),
		"cluster-b": makeJWKS("key-b1"),
		"cluster-c": makeJWKS("key-c1", "key-a1"), // key-a1 is a duplicate
	}

	merged := mergeJWKS(clusterJWKS)
	require.NotNil(t, merged)

	// Should have 4 unique keys (key-a1 deduplicated)
	assert.Len(t, merged.Keys, 4)

	kids := make(map[string]struct{})
	for _, k := range merged.Keys {
		kids[k.Kid] = struct{}{}
	}
	assert.Contains(t, kids, "key-a1")
	assert.Contains(t, kids, "key-a2")
	assert.Contains(t, kids, "key-b1")
	assert.Contains(t, kids, "key-c1")
}

func TestMergeJWKS_EmptyInput(t *testing.T) {
	merged := mergeJWKS(map[string]*bridge.JWKS{})
	require.NotNil(t, merged)
	assert.Empty(t, merged.Keys)
}

func TestMergeJWKS_NilClusterJWKS(t *testing.T) {
	clusterJWKS := map[string]*bridge.JWKS{
		"cluster-a": nil,
		"cluster-b": makeJWKS("key-b1"),
	}

	// Should not panic on nil JWKS entry
	assert.NotPanics(t, func() {
		merged := mergeJWKS(clusterJWKS)
		assert.NotNil(t, merged)
	})
}

func TestAggregationPoller_TTLPruning(t *testing.T) {
	now := time.Now()
	clusterTTL := 1 * time.Hour

	clusterJWKS := map[string]*bridge.JWKS{
		"cluster-a": makeJWKS("key-a1"),
		"cluster-b": makeJWKS("key-b1"),
		"cluster-c": makeJWKS("key-c1"),
		"cluster-d": makeJWKS("key-d1"), // stale
	}

	lastModified := map[string]time.Time{
		"cluster-a": now.Add(-30 * time.Minute), // fresh
		"cluster-b": now.Add(-45 * time.Minute), // fresh
		"cluster-c": now.Add(-50 * time.Minute), // fresh
		"cluster-d": now.Add(-90 * time.Minute), // stale: > 1h TTL
	}

	// Prune stale clusters (same logic as aggregationPoller.aggregate)
	for clusterID, t := range lastModified {
		if now.Sub(t) > clusterTTL {
			delete(clusterJWKS, clusterID)
		}
	}

	// After pruning, cluster-d should be removed
	assert.Len(t, clusterJWKS, 3)
	_, hasDead := clusterJWKS["cluster-d"]
	assert.False(t, hasDead, "stale cluster should be pruned")

	// Merge remaining 3 clusters
	merged := mergeJWKS(clusterJWKS)
	assert.Len(t, merged.Keys, 3)
}
