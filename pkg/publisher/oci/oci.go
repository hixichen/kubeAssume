// Package oci provides an OCI Object Storage implementation of the Publisher interface
package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
)

// Ensure ociPublisher implements iface.Publisher interface.
var _ iface.Publisher = (*ociPublisher)(nil)

// ociPublisher implements the Publisher interface for OCI Object Storage.
type ociPublisher struct {
	client objectstorage.ObjectStorageClient
	config Config
	logger *slog.Logger
}

// New creates a new OCI Object Storage publisher.
func New(ctx context.Context, config Config, logger *slog.Logger) (iface.Publisher, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid OCI config: %w", err)
	}

	var provider common.ConfigurationProvider
	var err error

	if config.UseInstancePrincipal {
		logger.Info("OCI publisher: using instance principal for authentication")
		provider = common.DefaultConfigProvider()
	} else {
		// Use default config file (~/.oci/config)
		logger.Info("OCI publisher: using default OCI configuration")
		provider = common.DefaultConfigProvider()
	}

	// Create OCI client
	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI client: %w", err)
	}

	return &ociPublisher{
		client: client,
		config: config,
		logger: logger,
	}, nil
}

// Publish uploads the discovery document and JWKS to OCI Object Storage.
func (o *ociPublisher) Publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error {
	o.logger.Debug("OCI publisher: publishing discovery document and JWKS")

	discoveryPath := o.config.GetDiscoveryPath()
	jwksPath := o.config.GetJWKSPath()

	// Publish discovery document
	if err := o.uploadObject(ctx, discoveryPath, discovery); err != nil {
		return fmt.Errorf("failed to upload discovery document to OCI: %w", err)
	}
	o.logger.Debug("OCI publisher: successfully uploaded discovery document",
		"bucket", o.config.Bucket,
		"path", discoveryPath,
	)

	// Publish JWKS
	if err := o.uploadObject(ctx, jwksPath, jwks); err != nil {
		return fmt.Errorf("failed to upload JWKS to OCI: %w", err)
	}
	o.logger.Debug("OCI publisher: successfully uploaded JWKS",
		"bucket", o.config.Bucket,
		"path", jwksPath,
	)

	return nil
}

// uploadObject marshals the object to JSON and uploads it to OCI Object Storage.
func (o *ociPublisher) uploadObject(ctx context.Context, objectName string, data interface{}) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data to JSON: %w", err)
	}

	contentType := o.config.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	cacheControl := o.config.CacheControl
	if cacheControl == "" {
		cacheControl = "max-age=300"
	}

	// Get object metadata for optimistic locking
	getReq := objectstorage.GetObjectRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
		ObjectName:    common.String(objectName),
	}

	getResp, err := o.client.GetObject(ctx, getReq)
	var ifMatchEtag *string
	if err != nil {
		// Object doesn't exist, use nil If-Match
		ifMatchEtag = nil
	} else {
		ifMatchEtag = getResp.ETag
	}

	// Prepare upload request
	putReq := objectstorage.PutObjectRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
		ObjectName:    common.String(objectName),
		PutObjectBody: io.NopCloser(bytes.NewReader(jsonData)),
		ContentType:   common.String(contentType),
		OpcMeta:       map[string]string{"Cache-Control": cacheControl},
	}

	if ifMatchEtag != nil {
		putReq.IfMatch = common.String(*ifMatchEtag)
	}

	// Upload with optimistic locking
	_, err = o.client.PutObject(ctx, putReq)
	if err != nil {
		// Check for precondition failed (another replica won)
		if serviceErr, ok := common.IsServiceError(err); ok && serviceErr.GetHTTPStatusCode() == 412 {
			o.logger.Debug("Object was updated by another replica, skipping update", "key", objectName)
			return nil
		}
		return fmt.Errorf("failed to upload object %s: %w", objectName, err)
	}

	return nil
}

// Validate checks configuration and permissions.
func (o *ociPublisher) Validate(ctx context.Context) error {
	o.logger.Debug("OCI publisher: validating configuration and permissions")

	// Check if bucket exists
	getReq := objectstorage.GetBucketRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
	}

	_, err := o.client.GetBucket(ctx, getReq)
	if err != nil {
		return fmt.Errorf("bucket '%s' is not accessible: %w", o.config.Bucket, err)
	}

	// Attempt to write a test object
	testObjectName := o.config.Prefix + "/kubeassume-test-write"
	testData := []byte("kubeassume-test-data")

	putReq := objectstorage.PutObjectRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
		ObjectName:    common.String(testObjectName),
		PutObjectBody: io.NopCloser(bytes.NewReader(testData)),
		ContentType:   common.String("text/plain"),
	}

	_, err = o.client.PutObject(ctx, putReq)
	if err != nil {
		return fmt.Errorf("write permission check failed for bucket %s: %w", o.config.Bucket, err)
	}

	o.logger.Debug("OCI publisher: successfully wrote test object",
		"bucket", o.config.Bucket,
		"path", testObjectName,
	)

	// Attempt to delete the test object
	delReq := objectstorage.DeleteObjectRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
		ObjectName:    common.String(testObjectName),
	}

	_, err = o.client.DeleteObject(ctx, delReq)
	if err != nil {
		o.logger.Warn("OCI publisher: failed to delete test object",
			"bucket", o.config.Bucket,
			"path", testObjectName,
			"error", err,
		)
	} else {
		o.logger.Debug("OCI publisher: successfully deleted test object",
			"bucket", o.config.Bucket,
			"path", testObjectName,
		)
	}

	o.logger.Info("OCI publisher: bucket is valid and permissions OK",
		"bucket", o.config.Bucket,
	)
	return nil
}

// GetPublicURL returns the public issuer URL.
func (o *ociPublisher) GetPublicURL() string {
	return o.config.GetPublicURL()
}

// HealthCheck verifies backend accessibility.
func (o *ociPublisher) HealthCheck(ctx context.Context) error {
	o.logger.Debug("OCI publisher: performing health check")

	// Try to get bucket properties
	getReq := objectstorage.GetBucketRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
	}

	_, err := o.client.GetBucket(ctx, getReq)
	if err != nil {
		return fmt.Errorf("OCI health check failed: unable to access bucket '%s': %w", o.config.Bucket, err)
	}

	o.logger.Debug("OCI publisher: health check successful")
	return nil
}

// Type returns the publisher type.
func (o *ociPublisher) Type() iface.PublisherType {
	return iface.PublisherTypeOCI
}

// Ensure ociPublisher implements iface.MultiClusterAggregator.
var _ iface.MultiClusterAggregator = (*ociPublisher)(nil)

// clusterListPrefix returns the prefix for listing cluster sub-paths.
func (o *ociPublisher) clusterListPrefix() string {
	if o.config.Prefix != "" {
		return o.config.Prefix + "/clusters/"
	}
	return "clusters/"
}

// ListClusterJWKS lists all cluster sub-paths under "clusters/" and returns parsed JWKS per clusterID.
func (o *ociPublisher) ListClusterJWKS(ctx context.Context) (map[string]*bridge.JWKS, error) {
	listPrefix := o.clusterListPrefix()
	delimiter := "/"
	listReq := objectstorage.ListObjectsRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
		Prefix:        common.String(listPrefix),
		Delimiter:     common.String(delimiter),
	}

	listResp, err := o.client.ListObjects(ctx, listReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster prefixes: %w", err)
	}

	clusterJWKS := make(map[string]*bridge.JWKS)
	for _, prefix := range listResp.Prefixes {
		trimmed := strings.TrimPrefix(prefix, listPrefix)
		clusterID := strings.TrimSuffix(trimmed, "/")
		if clusterID == "" {
			continue
		}

		jwksObjectName := o.config.GetClusterJWKSPath(clusterID)
		getReq := objectstorage.GetObjectRequest{
			NamespaceName: common.String(o.config.Namespace),
			BucketName:    common.String(o.config.Bucket),
			ObjectName:    common.String(jwksObjectName),
		}
		getResp, err := o.client.GetObject(ctx, getReq)
		if err != nil {
			o.logger.Warn("failed to fetch cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		data, err := io.ReadAll(getResp.Content)
		_ = getResp.Content.Close()
		if err != nil {
			o.logger.Warn("failed to read cluster JWKS body, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		var jwks bridge.JWKS
		if err := json.Unmarshal(data, &jwks); err != nil {
			o.logger.Warn("failed to decode cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		clusterJWKS[clusterID] = &jwks
	}
	return clusterJWKS, nil
}

// GetClusterLastModified returns the last-modified time of each cluster's JWKS object.
func (o *ociPublisher) GetClusterLastModified(ctx context.Context) (map[string]time.Time, error) {
	listPrefix := o.clusterListPrefix()
	delimiter := "/"
	listReq := objectstorage.ListObjectsRequest{
		NamespaceName: common.String(o.config.Namespace),
		BucketName:    common.String(o.config.Bucket),
		Prefix:        common.String(listPrefix),
		Delimiter:     common.String(delimiter),
	}

	listResp, err := o.client.ListObjects(ctx, listReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster prefixes: %w", err)
	}

	lastModified := make(map[string]time.Time)
	for _, prefix := range listResp.Prefixes {
		trimmed := strings.TrimPrefix(prefix, listPrefix)
		clusterID := strings.TrimSuffix(trimmed, "/")
		if clusterID == "" {
			continue
		}

		jwksObjectName := o.config.GetClusterJWKSPath(clusterID)
		headReq := objectstorage.HeadObjectRequest{
			NamespaceName: common.String(o.config.Namespace),
			BucketName:    common.String(o.config.Bucket),
			ObjectName:    common.String(jwksObjectName),
		}
		headResp, err := o.client.HeadObject(ctx, headReq)
		if err != nil {
			o.logger.Warn("failed to head cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		if headResp.LastModified != nil {
			lastModified[clusterID] = headResp.LastModified.Time
		}
	}
	return lastModified, nil
}

// PublishAggregatedJWKS writes the merged JWKS to the root JWKS path using optimistic locking.
func (o *ociPublisher) PublishAggregatedJWKS(ctx context.Context, merged *bridge.JWKS) error {
	rootPath := o.config.GetRootJWKSPath()
	return o.uploadObject(ctx, rootPath, merged)
}
