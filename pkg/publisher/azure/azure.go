// Package azure provides an Azure Blob Storage implementation of the Publisher interface
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
)

// Ensure azurePublisher implements iface.Publisher interface.
var _ iface.Publisher = (*azurePublisher)(nil)

// azurePublisher implements the Publisher interface for Azure Blob Storage.
type azurePublisher struct {
	client    *azblob.Client
	container string
	config    Config
	logger    *slog.Logger
}

// New creates a new Azure Blob Storage publisher.
func New(ctx context.Context, config Config, logger *slog.Logger) (iface.Publisher, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Azure config: %w", err)
	}

	var cred *azidentity.DefaultAzureCredential
	var err error

	if config.UseManagedIdentity {
		logger.Info("Azure publisher: using managed identity for authentication")
		cred, err = azidentity.NewDefaultAzureCredential(nil)
	} else {
		// Use default credential chain (falls back to environment variables)
		logger.Info("Azure publisher: using default credential chain")
		cred, err = azidentity.NewDefaultAzureCredential(nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	// Create service URL
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", config.StorageAccount)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Blob client: %w", err)
	}

	return &azurePublisher{
		client:    client,
		container: config.Container,
		config:    config,
		logger:    logger,
	}, nil
}

// Publish uploads the discovery document and JWKS to Azure Blob Storage.
func (a *azurePublisher) Publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error {
	a.logger.Debug("Azure publisher: publishing discovery document and JWKS")

	discoveryPath := a.config.GetDiscoveryPath()
	jwksPath := a.config.GetJWKSPath()

	// Publish discovery document
	if err := a.uploadObject(ctx, discoveryPath, discovery); err != nil {
		return fmt.Errorf("failed to upload discovery document to Azure: %w", err)
	}
	a.logger.Debug("Azure publisher: successfully uploaded discovery document",
		"container", a.container,
		"path", discoveryPath,
	)

	// Publish JWKS
	if err := a.uploadObject(ctx, jwksPath, jwks); err != nil {
		return fmt.Errorf("failed to upload JWKS to Azure: %w", err)
	}
	a.logger.Debug("Azure publisher: successfully uploaded JWKS",
		"container", a.container,
		"path", jwksPath,
	)

	return nil
}

// uploadObject marshals the object to JSON and uploads it to Azure Blob Storage with optimistic locking.
func (a *azurePublisher) uploadObject(ctx context.Context, blobPath string, data interface{}) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data to JSON: %w", err)
	}

	blobClient := a.client.ServiceClient().NewContainerClient(a.container).NewBlockBlobClient(blobPath)

	// Get current ETag for optimistic locking
	getResp, err := blobClient.GetProperties(ctx, nil)
	var ifMatch *azcore.ETag
	if err != nil {
		// Blob doesn't exist, we'll set If-Match to "*" for new blobs
		ifMatch = nil
	} else {
		ifMatch = getResp.ETag
	}

	contentType := a.config.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	cacheControl := a.config.CacheControl
	if cacheControl == "" {
		cacheControl = "max-age=300"
	}

	// Set headers
	headers := &blob.HTTPHeaders{
		BlobContentType:  &contentType,
		BlobCacheControl: &cacheControl,
	}

	uploadOptions := &blockblob.UploadOptions{
		HTTPHeaders: headers,
	}

	if ifMatch != nil {
		uploadOptions.AccessConditions = &blob.AccessConditions{
			ModifiedAccessConditions: &blob.ModifiedAccessConditions{
				IfMatch: ifMatch,
			},
		}
	}

	// Upload with optimistic locking
	_, err = blobClient.Upload(ctx, streaming.NopCloser(bytes.NewReader(jsonData)), uploadOptions)
	if err != nil {
		// Check for precondition failed (another replica won)
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == 412 {
			a.logger.Debug("Object was updated by another replica, skipping update", "key", blobPath)
			return nil
		}
		return fmt.Errorf("failed to upload blob %s: %w", blobPath, err)
	}

	// Note: Public access in Azure is configured at the container level, not per-blob

	return nil
}

// Validate checks configuration and permissions.
func (a *azurePublisher) Validate(ctx context.Context) error {
	a.logger.Debug("Azure publisher: validating configuration and permissions")

	// Check if container exists
	containerClient := a.client.ServiceClient().NewContainerClient(a.container)
	_, err := containerClient.GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("container '%s' is not accessible: %w", a.container, err)
	}

	// Attempt to write a test blob
	testBlobPath := a.config.Prefix + "/kubeassume-test-write"
	testData := []byte("kubeassume-test-data")

	testBlobClient := containerClient.NewBlockBlobClient(testBlobPath)
	_, err = testBlobClient.UploadBuffer(ctx, testData, nil)
	if err != nil {
		return fmt.Errorf("write permission check failed for container %s: %w", a.container, err)
	}

	a.logger.Debug("Azure publisher: successfully wrote test blob",
		"container", a.container,
		"path", testBlobPath,
	)

	// Attempt to delete the test blob
	_, err = testBlobClient.Delete(ctx, nil)
	if err != nil {
		a.logger.Warn("Azure publisher: failed to delete test blob",
			"container", a.container,
			"path", testBlobPath,
			"error", err,
		)
	} else {
		a.logger.Debug("Azure publisher: successfully deleted test blob",
			"container", a.container,
			"path", testBlobPath,
		)
	}

	a.logger.Info("Azure publisher: container is valid and permissions OK",
		"container", a.container,
	)
	return nil
}

// GetPublicURL returns the public issuer URL.
func (a *azurePublisher) GetPublicURL() string {
	return a.config.GetPublicURL()
}

// HealthCheck verifies backend accessibility.
func (a *azurePublisher) HealthCheck(ctx context.Context) error {
	a.logger.Debug("Azure publisher: performing health check")

	// Try to get container properties to verify connectivity
	containerClient := a.client.ServiceClient().NewContainerClient(a.container)
	_, err := containerClient.GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("azure health check failed: unable to access container '%s': %w", a.container, err)
	}

	a.logger.Debug("Azure publisher: health check successful")
	return nil
}

// Type returns the publisher type.
func (a *azurePublisher) Type() iface.PublisherType {
	return iface.PublisherTypeAzure
}

// Ensure azurePublisher implements iface.MultiClusterAggregator.
var _ iface.MultiClusterAggregator = (*azurePublisher)(nil)

// ListClusterJWKS lists all cluster sub-paths under "clusters/" and returns parsed JWKS per clusterID.
func (a *azurePublisher) ListClusterJWKS(ctx context.Context) (map[string]*bridge.JWKS, error) {
	clustersPrefix := a.config.GetClusterJWKSPath("") // gets Prefix/clusters/
	// Trim trailing slash from the joined path separator
	clustersPrefix = strings.TrimSuffix(clustersPrefix, "/")
	// Use the prefix up to "clusters/" as delimiter hierarchy
	clusterListPrefix := ""
	if a.config.Prefix != "" {
		clusterListPrefix = a.config.Prefix + "/clusters/"
	} else {
		clusterListPrefix = "clusters/"
	}

	containerClient := a.client.ServiceClient().NewContainerClient(a.container)
	pager := containerClient.NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{
		Prefix: &clusterListPrefix,
	})

	clusterJWKS := make(map[string]*bridge.JWKS)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list cluster blobs: %w", err)
		}
		for _, prefix := range page.Segment.BlobPrefixes {
			if prefix.Name == nil {
				continue
			}
			trimmed := strings.TrimPrefix(*prefix.Name, clusterListPrefix)
			clusterID := strings.TrimSuffix(trimmed, "/")
			if clusterID == "" {
				continue
			}

			jwksBlobPath := a.config.GetClusterJWKSPath(clusterID)
			blobClient := containerClient.NewBlockBlobClient(jwksBlobPath)
			resp, err := blobClient.DownloadStream(ctx, nil)
			if err != nil {
				a.logger.Warn("failed to download cluster JWKS, skipping", "clusterID", clusterID, "error", err)
				continue
			}
			data, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				a.logger.Warn("failed to read cluster JWKS body, skipping", "clusterID", clusterID, "error", err)
				continue
			}
			var jwks bridge.JWKS
			if err := json.Unmarshal(data, &jwks); err != nil {
				a.logger.Warn("failed to decode cluster JWKS, skipping", "clusterID", clusterID, "error", err)
				continue
			}
			clusterJWKS[clusterID] = &jwks
		}
	}
	_ = clustersPrefix // suppress unused warning
	return clusterJWKS, nil
}

// GetClusterLastModified returns the last-modified time of each cluster's JWKS object.
func (a *azurePublisher) GetClusterLastModified(ctx context.Context) (map[string]time.Time, error) {
	clusterListPrefix := ""
	if a.config.Prefix != "" {
		clusterListPrefix = a.config.Prefix + "/clusters/"
	} else {
		clusterListPrefix = "clusters/"
	}

	containerClient := a.client.ServiceClient().NewContainerClient(a.container)
	pager := containerClient.NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{
		Prefix: &clusterListPrefix,
	})

	lastModified := make(map[string]time.Time)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list cluster blobs: %w", err)
		}
		for _, prefix := range page.Segment.BlobPrefixes {
			if prefix.Name == nil {
				continue
			}
			trimmed := strings.TrimPrefix(*prefix.Name, clusterListPrefix)
			clusterID := strings.TrimSuffix(trimmed, "/")
			if clusterID == "" {
				continue
			}

			jwksBlobPath := a.config.GetClusterJWKSPath(clusterID)
			blobClient := containerClient.NewBlockBlobClient(jwksBlobPath)
			props, err := blobClient.GetProperties(ctx, nil)
			if err != nil {
				a.logger.Warn("failed to get cluster JWKS properties, skipping", "clusterID", clusterID, "error", err)
				continue
			}
			if props.LastModified != nil {
				lastModified[clusterID] = *props.LastModified
			}
		}
	}
	return lastModified, nil
}

// PublishAggregatedJWKS writes the merged JWKS to the root JWKS path using optimistic locking.
func (a *azurePublisher) PublishAggregatedJWKS(ctx context.Context, merged *bridge.JWKS) error {
	rootPath := a.config.GetRootJWKSPath()
	return a.uploadObject(ctx, rootPath, merged)
}
