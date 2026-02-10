// Package iface provides the publisher interface
package iface

import (
	"context"
	"time"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
)

// PublisherType represents the type of publisher backend.
type PublisherType string

const (
	// PublisherTypeS3 is AWS S3.
	PublisherTypeS3 PublisherType = "s3"
	// PublisherTypeGCS is Google Cloud Storage.
	PublisherTypeGCS PublisherType = "gcs"
	// PublisherTypeAzure is Azure Blob Storage.
	PublisherTypeAzure PublisherType = "azure"
	// PublisherTypeOCI is Oracle Cloud Infrastructure Object Storage.
	PublisherTypeOCI PublisherType = "oci"
)

// Publisher defines the interface for publishing OIDC metadata.
type Publisher interface {
	// Publish uploads the discovery document and JWKS to the backend
	Publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error

	// Validate checks that the publisher is properly configured
	// and has necessary permissions
	Validate(ctx context.Context) error

	// GetPublicURL returns the public URL where OIDC metadata is accessible
	GetPublicURL() string

	// HealthCheck verifies the publisher backend is accessible
	HealthCheck(ctx context.Context) error

	// Type returns the publisher type (s3, gcs, azure, oci)
	Type() PublisherType
}

// MultiClusterAggregator is implemented by publishers when clusterGroup is set.
// Only the elected leader calls these methods.
type MultiClusterAggregator interface {
	// ListClusterJWKS lists all cluster sub-paths under "clusters/" and reads each JWKS.
	// Returns a map from clusterID to JWKS.
	ListClusterJWKS(ctx context.Context) (map[string]*bridge.JWKS, error)

	// GetClusterLastModified returns last-modified time per clusterID for TTL pruning.
	GetClusterLastModified(ctx context.Context) (map[string]time.Time, error)

	// PublishAggregatedJWKS writes merged JWKS to root openid/v1/jwks with optimistic locking.
	PublishAggregatedJWKS(ctx context.Context, merged *bridge.JWKS) error
}
