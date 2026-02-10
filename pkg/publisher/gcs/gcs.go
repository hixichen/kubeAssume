// Package gcs provides a GCS implementation of the Publisher interface
package gcs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
)

// Ensure gcsPublisher implements iface.Publisher interface.
var _ iface.Publisher = (*gcsPublisher)(nil)

// gcsPublisher implements the Publisher interface for Google Cloud Storage.
type gcsPublisher struct {
	client       *storage.Client
	config       Config
	bucketHandle *storage.BucketHandle
	logger       *slog.Logger
}

// New creates a new GCS publisher.
func New(ctx context.Context, config Config, logger *slog.Logger) (iface.Publisher, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid GCS config: %w", err)
	}

	var opts []option.ClientOption

	if config.UseWorkloadIdentity {
		logger.Info("GCS publisher: using workload identity for authentication")
		// Default credential chain will use Workload Identity if configured
		// No explicit options needed here for default behavior.
	} else {
		// Fallback for explicit credentials if needed, or other auth methods.
		// For now, we assume Workload Identity is the primary method.
		logger.Warn("GCS publisher: not using workload identity. Ensure appropriate GCP credentials are available.")
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	publisher := &gcsPublisher{
		client:       client,
		config:       config,
		bucketHandle: client.Bucket(config.Bucket),
		logger:       logger,
	}

	return publisher, nil
}

// prefixedKey prepends the configured prefix to a relative object key.
func (g *gcsPublisher) prefixedKey(key string) string {
	if g.config.Prefix != "" {
		return g.config.Prefix + "/" + key
	}
	return key
}

// Publish uploads the discovery document and JWKS to the GCS bucket.
func (g *gcsPublisher) Publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error {
	g.logger.Debug("GCS publisher: publishing discovery document and JWKS")

	discoveryPath := g.prefixedKey(g.config.GetDiscoveryPath())
	jwksPath := g.prefixedKey(g.config.GetJWKSPath())

	// Publish discovery document
	if err := g.uploadObject(ctx, discoveryPath, discovery); err != nil {
		return fmt.Errorf("failed to upload discovery document to GCS: %w", err)
	}
	g.logger.Debug("GCS publisher: successfully uploaded discovery document",
		"bucket", g.config.Bucket,
		"path", discoveryPath,
	)

	// Publish JWKS
	if err := g.uploadObject(ctx, jwksPath, jwks); err != nil {
		return fmt.Errorf("failed to upload JWKS to GCS: %w", err)
	}
	g.logger.Debug("GCS publisher: successfully uploaded JWKS",
		"bucket", g.config.Bucket,
		"path", jwksPath,
	)

	return nil
}

// uploadObject marshals the object to JSON and uploads it to GCS with optimistic locking.
func (g *gcsPublisher) uploadObject(ctx context.Context, path string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data to JSON: %w", err)
	}

	obj := g.bucketHandle.Object(path)

	// Get current generation for optimistic locking
	attrs, err := obj.Attrs(ctx)
	var generation int64
	if err != nil {
		if err != storage.ErrObjectNotExist {
			return fmt.Errorf("failed to get object attributes for %s: %w", path, err)
		}
		// Object doesn't exist, so we expect generation 0
		generation = 0
	} else {
		generation = attrs.Generation
	}

	// Set up writer with precondition
	wc := obj.If(storage.Conditions{GenerationMatch: generation}).NewWriter(ctx)
	wc.ContentType = g.config.ContentType
	wc.CacheControl = g.config.CacheControl

	if _, err := wc.Write(jsonData); err != nil {
		return fmt.Errorf("failed to write data to GCS object %s: %w", path, err)
	}
	if err := wc.Close(); err != nil {
		var gerr *googleapi.Error
		if errors.As(err, &gerr) && gerr.Code == 412 {
			g.logger.Debug("Object was updated by another replica, skipping update", "key", path)
			return nil // Not an error, another replica won
		}
		return fmt.Errorf("failed to close GCS object writer for %s: %w", path, err)
	}
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return fmt.Errorf("failed to set public read ACL for GCS object %s: %w", path, err)
	}

	return nil
}

// Validate checks configuration and permissions.
func (g *gcsPublisher) Validate(ctx context.Context) error {
	g.logger.Debug("GCS publisher: validating configuration and permissions")

	// Check if bucket exists
	if _, err := g.bucketHandle.Attrs(ctx); err != nil {
		if err == storage.ErrBucketNotExist {
			return fmt.Errorf("GCS bucket '%s' does not exist", g.config.Bucket)
		}
		return fmt.Errorf("failed to get GCS bucket attributes for '%s': %w", g.config.Bucket, err)
	}

	// Attempt to write a dummy object to verify write permissions
	testObjectName := g.config.Prefix + "/kubeassume-test-write-" + time.Now().Format("20060102150405")
	testData := []byte("kubeassume-test-data")

	testObj := g.bucketHandle.Object(testObjectName)
	testWriter := testObj.NewWriter(ctx)
	testWriter.ContentType = "text/plain"
	testWriter.CacheControl = "no-cache"

	if _, err := testWriter.Write(testData); err != nil {
		return fmt.Errorf("failed to write test object to GCS bucket '%s'. Check write permissions: %w", g.config.Bucket, err)
	}
	if err := testWriter.Close(); err != nil {
		return fmt.Errorf("failed to close test object writer to GCS bucket '%s': %w", g.config.Bucket, err)
	}
	g.logger.Debug("GCS publisher: successfully wrote test object",
		"bucket", g.config.Bucket,
		"path", testObjectName,
	)

	// Attempt to delete the dummy object
	if err := testObj.Delete(ctx); err != nil {
		g.logger.Warn("GCS publisher: failed to delete test object. Manual cleanup may be required.",
			"bucket", g.config.Bucket,
			"path", testObjectName,
			"error", err,
		)
	} else {
		g.logger.Debug("GCS publisher: successfully deleted test object",
			"bucket", g.config.Bucket,
			"path", testObjectName,
		)
	}

	// Verify public readability by trying to read a path (e.g., discovery document path)
	// This might fail if nothing has been published yet, so we'll check it more loosely.
	// A more robust check might involve trying to read a known public object if one exists,
	// but for now, we'll assume the write/delete check is sufficient for permissions.
	// The `PredefinedACL: storage.ACLPublicRead` in `uploadObject` is what ensures public readability.

	g.logger.Info("GCS publisher: bucket is valid and permissions OK",
		"bucket", g.config.Bucket,
	)
	return nil
}

// GetPublicURL returns the public issuer URL.
func (g *gcsPublisher) GetPublicURL() string {
	return g.config.GetPublicURL()
}

// HealthCheck verifies backend accessibility.
func (g *gcsPublisher) HealthCheck(ctx context.Context) error {
	g.logger.Debug("GCS publisher: performing health check")

	// Try to list objects to verify connectivity and basic read access
	it := g.bucketHandle.Objects(ctx, nil)
	_, err := it.Next()
	if err != nil && err != iterator.Done {
		return fmt.Errorf("GCS health check failed: unable to list objects in bucket '%s': %w", g.config.Bucket, err)
	}

	g.logger.Debug("GCS publisher: health check successful")
	return nil
}

// Type returns the publisher type (s3, gcs, azure, oci).
func (g *gcsPublisher) Type() iface.PublisherType {
	return iface.PublisherTypeGCS
}

// Ensure gcsPublisher implements iface.MultiClusterAggregator.
var _ iface.MultiClusterAggregator = (*gcsPublisher)(nil)

// ListClusterJWKS lists all cluster sub-paths under "clusters/" and returns parsed JWKS per clusterID.
func (g *gcsPublisher) ListClusterJWKS(ctx context.Context) (map[string]*bridge.JWKS, error) {
	clustersPrefix := g.prefixedKey("clusters/")
	query := &storage.Query{Prefix: clustersPrefix, Delimiter: "/"}

	clusterIDs, err := g.listClusterIDs(ctx, query, clustersPrefix)
	if err != nil {
		return nil, err
	}

	clusterJWKS := make(map[string]*bridge.JWKS)
	for _, clusterID := range clusterIDs {
		jwksKey := g.prefixedKey(g.config.GetClusterJWKSPath(clusterID))
		obj := g.bucketHandle.Object(jwksKey)
		r, err := obj.NewReader(ctx)
		if err != nil {
			g.logger.Warn("failed to read cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			g.logger.Warn("failed to read cluster JWKS body, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		var jwks bridge.JWKS
		if err := json.Unmarshal(data, &jwks); err != nil {
			g.logger.Warn("failed to decode cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		clusterJWKS[clusterID] = &jwks
	}
	return clusterJWKS, nil
}

// GetClusterLastModified returns the last-modified time of each cluster's JWKS object.
func (g *gcsPublisher) GetClusterLastModified(ctx context.Context) (map[string]time.Time, error) {
	clustersPrefix := g.prefixedKey("clusters/")
	query := &storage.Query{Prefix: clustersPrefix, Delimiter: "/"}

	clusterIDs, err := g.listClusterIDs(ctx, query, clustersPrefix)
	if err != nil {
		return nil, err
	}

	lastModified := make(map[string]time.Time)
	for _, clusterID := range clusterIDs {
		jwksKey := g.prefixedKey(g.config.GetClusterJWKSPath(clusterID))
		attrs, err := g.bucketHandle.Object(jwksKey).Attrs(ctx)
		if err != nil {
			g.logger.Warn("failed to get cluster JWKS attrs, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		lastModified[clusterID] = attrs.Updated
	}
	return lastModified, nil
}

// PublishAggregatedJWKS writes the merged JWKS to the root JWKS path using optimistic locking.
func (g *gcsPublisher) PublishAggregatedJWKS(ctx context.Context, merged *bridge.JWKS) error {
	rootKey := g.prefixedKey(g.config.GetRootJWKSPath())
	return g.uploadObject(ctx, rootKey, merged)
}

// listClusterIDs lists cluster IDs from the "clusters/" prefix using delimiter listing.
func (g *gcsPublisher) listClusterIDs(ctx context.Context, query *storage.Query, clustersPrefix string) ([]string, error) {
	it := g.bucketHandle.Objects(ctx, query)
	var clusterIDs []string
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list cluster prefixes: %w", err)
		}
		if attrs.Prefix == "" {
			continue
		}
		trimmed := strings.TrimPrefix(attrs.Prefix, clustersPrefix)
		clusterID := strings.TrimSuffix(trimmed, "/")
		if clusterID != "" {
			clusterIDs = append(clusterIDs, clusterID)
		}
	}
	return clusterIDs, nil
}
