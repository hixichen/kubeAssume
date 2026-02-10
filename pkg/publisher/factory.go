package publisher

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hixichen/kube-iam-assume/pkg/config"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/azure"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/gcs"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/oci"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/s3"
)

// Factory creates Publisher instances based on configuration.
type Factory struct {
	logger *slog.Logger
}

// NewFactory creates a new publisher factory.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{
		logger: logger,
	}
}

// Create creates a Publisher based on the configuration.
func (f *Factory) Create(ctx context.Context, cfg *config.Config) (iface.Publisher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	switch iface.PublisherType(cfg.Publisher.Type) {
	case iface.PublisherTypeS3:
		return f.createS3Publisher(ctx, cfg.Publisher.S3, cfg.Controller.ClusterGroup, cfg.Controller.ClusterID)
	case iface.PublisherTypeGCS:
		return f.createGCSPublisher(ctx, cfg.Publisher.GCS, cfg.Controller.ClusterGroup, cfg.Controller.ClusterID)
	case iface.PublisherTypeAzure:
		return f.createAzurePublisher(ctx, cfg.Publisher.Azure, cfg.Controller.ClusterGroup, cfg.Controller.ClusterID)
	case iface.PublisherTypeOCI:
		return f.createOCIPublisher(ctx, cfg.Publisher.OCI, cfg.Controller.ClusterGroup, cfg.Controller.ClusterID)
	default:
		return nil, fmt.Errorf("unsupported publisher type: %s", cfg.Publisher.Type)
	}
}

// createS3Publisher creates an S3 publisher.
func (f *Factory) createS3Publisher(ctx context.Context, cfg *config.S3Config, clusterGroup, clusterID string) (iface.Publisher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("S3 configuration is required")
	}

	s3Cfg := s3.Config{
		Bucket:         cfg.Bucket,
		Region:         cfg.Region,
		Endpoint:       cfg.Endpoint,
		ForcePathStyle: cfg.ForcePathStyle,
		Prefix:         cfg.Prefix,
		UseIRSA:        cfg.UseIRSA,
		CacheControl:   cfg.CacheControl,
		ContentType:    cfg.ContentType,
	}

	// When clusterGroup is set, override prefix with group name and enable multi-cluster mode
	if clusterGroup != "" {
		s3Cfg.Prefix = clusterGroup
		s3Cfg.MultiClusterEnabled = true
		s3Cfg.ClusterID = clusterID
	}

	pub, err := s3.New(ctx, s3Cfg, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 publisher: %w", err)
	}

	return pub, nil
}

// createGCSPublisher creates a GCS publisher.
func (f *Factory) createGCSPublisher(ctx context.Context, cfg *config.GCSConfig, clusterGroup, clusterID string) (iface.Publisher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("GCS configuration is required")
	}

	gcsCfg := gcs.Config{
		Bucket:              cfg.Bucket,
		Project:             cfg.Project,
		Prefix:              cfg.Prefix,
		UseWorkloadIdentity: cfg.UseWorkloadIdentity,
		CacheControl:        cfg.CacheControl,
		ContentType:         cfg.ContentType,
	}

	// When clusterGroup is set, override prefix with group name and enable multi-cluster mode
	if clusterGroup != "" {
		gcsCfg.Prefix = clusterGroup
		gcsCfg.MultiClusterEnabled = true
		gcsCfg.ClusterID = clusterID
	}

	pub, err := gcs.New(ctx, gcsCfg, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS publisher: %w", err)
	}

	return pub, nil
}

// createAzurePublisher creates an Azure Blob Storage publisher.
func (f *Factory) createAzurePublisher(ctx context.Context, cfg *config.AzureConfig, clusterGroup, clusterID string) (iface.Publisher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("azure configuration is required")
	}

	azureCfg := azure.Config{
		StorageAccount:     cfg.StorageAccount,
		Container:          cfg.Container,
		Prefix:             cfg.Prefix,
		UseManagedIdentity: cfg.UseManagedIdentity,
		TenantID:           cfg.TenantID,
		ClientID:           cfg.ClientID,
		ClientSecret:       cfg.ClientSecret,
		CacheControl:       cfg.CacheControl,
		ContentType:        cfg.ContentType,
	}

	// When clusterGroup is set, override prefix with group name and enable multi-cluster mode
	if clusterGroup != "" {
		azureCfg.Prefix = clusterGroup
		azureCfg.MultiClusterEnabled = true
		azureCfg.ClusterID = clusterID
	}

	pub, err := azure.New(ctx, azureCfg, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure publisher: %w", err)
	}

	return pub, nil
}

// createOCIPublisher creates an OCI Object Storage publisher.
func (f *Factory) createOCIPublisher(ctx context.Context, cfg *config.OCIConfig, clusterGroup, clusterID string) (iface.Publisher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("OCI configuration is required")
	}

	ociCfg := oci.Config{
		Bucket:               cfg.Bucket,
		Namespace:            cfg.Namespace,
		Region:               cfg.Region,
		Prefix:               cfg.Prefix,
		UseInstancePrincipal: cfg.UseInstancePrincipal,
		UserID:               cfg.UserID,
		Fingerprint:          cfg.Fingerprint,
		KeyFile:              cfg.KeyFile,
		TenancyID:            cfg.TenancyID,
		CacheControl:         cfg.CacheControl,
		ContentType:          cfg.ContentType,
	}

	// When clusterGroup is set, override prefix with group name and enable multi-cluster mode
	if clusterGroup != "" {
		ociCfg.Prefix = clusterGroup
		ociCfg.MultiClusterEnabled = true
		ociCfg.ClusterID = clusterID
	}

	pub, err := oci.New(ctx, ociCfg, f.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI publisher: %w", err)
	}

	return pub, nil
}
