// Package publisher defines the interface for publishing OIDC metadata
// to various backends (S3, GCS, Azure Blob, etc.)
package publisher

import (
	"context"

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
	// PublisherTypeOCI is OCI Object Storage.
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

// PublishResult contains the result of a publish operation.
type PublishResult struct {
	// DiscoveryURL is the URL of the published discovery document
	DiscoveryURL string
	// JWKSURL is the URL of the published JWKS
	JWKSURL string
	// PublishedAt is the timestamp when the content was published
	PublishedAt string
}
