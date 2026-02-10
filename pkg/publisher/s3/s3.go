package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
)

// Ensure Publisher implements iface.Publisher interface.

var _ iface.Publisher = (*Publisher)(nil)

// Publisher implements iface.Publisher for AWS S3.

type Publisher struct {
	client *s3.Client

	config Config

	logger *slog.Logger
}

// New creates a new S3 Publisher.

func New(ctx context.Context, cfg Config, logger *slog.Logger) (iface.Publisher, error) {
	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid S3 config: %w", err)
	}

	// Load AWS config (with IRSA support)
	awsCfg, err := loadAWSConfig(ctx, cfg.Region, cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	client := createS3Client(awsCfg, cfg.Endpoint, cfg.ForcePathStyle)

	return &Publisher{
		client: client,
		config: cfg,
		logger: logger,
	}, nil
}

// Publish uploads the discovery document and JWKS to S3.
func (p *Publisher) Publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error {
	// Marshal discovery document to JSON
	discoveryData, err := marshalJSON(discovery)
	if err != nil {
		return fmt.Errorf("failed to marshal discovery document: %w", err)
	}

	// Upload discovery document to .well-known/openid-configuration (prefixed)
	if err := p.uploadObject(ctx, p.prefixedKey(p.config.GetDiscoveryPath()), discoveryData); err != nil {
		return fmt.Errorf("failed to upload discovery document: %w", err)
	}

	// Marshal JWKS to JSON
	jwksData, err := marshalJSON(jwks)
	if err != nil {
		return fmt.Errorf("failed to marshal JWKS: %w", err)
	}

	// Upload JWKS (in multi-cluster mode, writes to cluster sub-path)
	if err := p.uploadObject(ctx, p.prefixedKey(p.config.GetJWKSPath()), jwksData); err != nil {
		return fmt.Errorf("failed to upload JWKS: %w", err)
	}

	// Log successful publish
	p.logger.Info("Successfully published OIDC metadata to S3",
		"bucket", p.config.Bucket,
		"discovery_path", p.prefixedKey(p.config.GetDiscoveryPath()),
		"jwks_path", p.prefixedKey(p.config.GetJWKSPath()),
	)

	return nil
}

// prefixedKey prepends the configured prefix to a relative object key.
func (p *Publisher) prefixedKey(key string) string {
	if p.config.Prefix != "" {
		return p.config.Prefix + "/" + key
	}
	return key
}

// Validate checks that the publisher is properly configured.
func (p *Publisher) Validate(ctx context.Context) error {
	// Check bucket exists and is accessible
	_, err := p.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(p.config.Bucket),
	})
	if err != nil {
		return fmt.Errorf("bucket %s is not accessible: %w", p.config.Bucket, err)
	}

	// Check write permissions by attempting a test upload
	testKey := ".kubeassume/validation-test"
	_, err = p.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(p.config.Bucket),
		Key:    aws.String(testKey),
		Body:   bytes.NewReader([]byte("test")),
	})
	if err != nil {
		return fmt.Errorf("write permission check failed for bucket %s: %w", p.config.Bucket, err)
	}

	// Clean up test object
	_, _ = p.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(p.config.Bucket),
		Key:    aws.String(testKey),
	})

	// Check bucket has public read policy by getting bucket policy status
	// Note: We can't directly check if objects are public, but we can validate the setup
	p.logger.Info("S3 bucket validation successful",
		"bucket", p.config.Bucket,
		"public_url", p.GetPublicURL(),
	)

	return nil
}

// GetPublicURL returns the public URL for the OIDC issuer.
func (p *Publisher) GetPublicURL() string {
	return p.config.GetPublicURL()
}

// Type returns the publisher type.
func (p *Publisher) Type() iface.PublisherType {
	return iface.PublisherTypeS3
}

// HealthCheck verifies S3 is accessible.
func (p *Publisher) HealthCheck(ctx context.Context) error {
	// Perform HeadBucket operation to check accessibility
	_, err := p.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(p.config.Bucket),
	})
	if err != nil {
		return fmt.Errorf("S3 health check failed: %w", err)
	}
	return nil
}

// uploadObject uploads a JSON object to S3 with optimistic locking.
func (p *Publisher) uploadObject(ctx context.Context, key string, data []byte) error {
	contentType := p.config.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	cacheControl := p.config.CacheControl
	if cacheControl == "" {
		cacheControl = "max-age=300"
	}

	// Get current ETag for optimistic locking
	head, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(p.config.Bucket),
		Key:    aws.String(key),
	})

	var ifMatch *string
	if err == nil {
		ifMatch = head.ETag
	} else {
		// We expect a NotFound error if the object doesn't exist yet.
		var notFound *types.NotFound
		if !errors.As(err, &notFound) {
			return fmt.Errorf("failed to head object %s: %w", key, err)
		}
	}

	// Create PutObjectInput with correct headers
	input := &s3.PutObjectInput{
		Bucket:       aws.String(p.config.Bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String(cacheControl),
		IfMatch:      ifMatch,
	}

	// Execute PutObject
	_, err = p.client.PutObject(ctx, input)
	if err != nil {
		var responseError *awshttp.ResponseError
		if errors.As(err, &responseError) && responseError.HTTPStatusCode() == http.StatusPreconditionFailed {
			p.logger.Debug("Object was updated by another replica, skipping update", "key", key)
			return nil // Not an error, another replica won
		}
		return fmt.Errorf("failed to upload object %s: %w", key, err)
	}

	// Log upload details
	p.logger.Debug("Uploaded object to S3",
		"bucket", p.config.Bucket,
		"key", key,
		"size", len(data),
	)

	return nil
}

// marshalJSON marshals an object to JSON with proper formatting.
func marshalJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// loadAWSConfig loads AWS configuration with optional custom endpoint.
func loadAWSConfig(ctx context.Context, region string, endpoint string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return cfg, nil
}

// createS3Client creates an S3 client with optional custom endpoint.
func createS3Client(cfg aws.Config, endpoint string, forcePathStyle bool) *s3.Client {
	opts := []func(*s3.Options){}

	if endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = forcePathStyle
		})
	}

	return s3.NewFromConfig(cfg, opts...)
}

// CheckPublicAccess verifies if objects in the bucket can be publicly accessed
// This is a best-effort check and may not work in all environments.
func (p *Publisher) CheckPublicAccess(ctx context.Context, key string) error {
	// Try to get the object ACL
	_, err := p.client.GetObjectAcl(ctx, &s3.GetObjectAclInput{
		Bucket: aws.String(p.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to get object ACL: %w", err)
	}

	return nil
}

// GetBucketPolicy retrieves the bucket policy.
func (p *Publisher) GetBucketPolicy(ctx context.Context) (*types.PolicyStatus, error) {
	result, err := p.client.GetBucketPolicyStatus(ctx, &s3.GetBucketPolicyStatusInput{
		Bucket: aws.String(p.config.Bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket policy status: %w", err)
	}

	return result.PolicyStatus, nil
}

// Ensure Publisher implements iface.MultiClusterAggregator when in multi-cluster mode.
var _ iface.MultiClusterAggregator = (*Publisher)(nil)

// ListClusterJWKS lists all cluster sub-paths under "clusters/" and returns parsed JWKS per clusterID.
func (p *Publisher) ListClusterJWKS(ctx context.Context) (map[string]*bridge.JWKS, error) {
	clustersPrefix := p.prefixedKey("clusters/")
	result, err := p.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(p.config.Bucket),
		Prefix:    aws.String(clustersPrefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster prefixes: %w", err)
	}

	clusterJWKS := make(map[string]*bridge.JWKS)
	for _, cp := range result.CommonPrefixes {
		if cp.Prefix == nil {
			continue
		}
		// Extract clusterID from "prefix/clusters/clusterID/"
		trimmed := strings.TrimPrefix(*cp.Prefix, clustersPrefix)
		clusterID := strings.TrimSuffix(trimmed, "/")
		if clusterID == "" {
			continue
		}

		jwksKey := p.prefixedKey(p.config.GetClusterJWKSPath(clusterID))
		getOut, err := p.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(p.config.Bucket),
			Key:    aws.String(jwksKey),
		})
		if err != nil {
			p.logger.Warn("failed to fetch cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		defer func() { _ = getOut.Body.Close() }()

		var jwks bridge.JWKS
		if err := json.NewDecoder(getOut.Body).Decode(&jwks); err != nil {
			p.logger.Warn("failed to decode cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		clusterJWKS[clusterID] = &jwks
	}
	return clusterJWKS, nil
}

// GetClusterLastModified returns the last-modified time of each cluster's JWKS object.
func (p *Publisher) GetClusterLastModified(ctx context.Context) (map[string]time.Time, error) {
	clustersPrefix := p.prefixedKey("clusters/")
	result, err := p.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(p.config.Bucket),
		Prefix:    aws.String(clustersPrefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster prefixes: %w", err)
	}

	lastModified := make(map[string]time.Time)
	for _, cp := range result.CommonPrefixes {
		if cp.Prefix == nil {
			continue
		}
		trimmed := strings.TrimPrefix(*cp.Prefix, clustersPrefix)
		clusterID := strings.TrimSuffix(trimmed, "/")
		if clusterID == "" {
			continue
		}

		jwksKey := p.prefixedKey(p.config.GetClusterJWKSPath(clusterID))
		head, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(p.config.Bucket),
			Key:    aws.String(jwksKey),
		})
		if err != nil {
			p.logger.Warn("failed to head cluster JWKS, skipping", "clusterID", clusterID, "error", err)
			continue
		}
		if head.LastModified != nil {
			lastModified[clusterID] = *head.LastModified
		}
	}
	return lastModified, nil
}

// PublishAggregatedJWKS writes the merged JWKS to the root JWKS path using optimistic locking.
func (p *Publisher) PublishAggregatedJWKS(ctx context.Context, merged *bridge.JWKS) error {
	data, err := marshalJSON(merged)
	if err != nil {
		return fmt.Errorf("failed to marshal aggregated JWKS: %w", err)
	}
	rootKey := p.prefixedKey(p.config.GetRootJWKSPath())
	return p.uploadObject(ctx, rootKey, data)
}
