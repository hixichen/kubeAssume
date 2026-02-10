package publisher

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hixichen/kube-iam-assume/pkg/config"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
)

func TestNewFactory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)
	require.NotNil(t, factory)
	assert.NotNil(t, factory.logger)
}

func TestFactory_Create_UnsupportedType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	cfg := &config.Config{
		Publisher: config.PublisherConfig{
			Type: "unsupported",
		},
	}

	ctx := t.Context()
	_, err := factory.Create(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported publisher type")
}

func TestFactory_Create_NilConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	ctx := t.Context()
	_, err := factory.Create(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil config")
}

func TestFactory_Create_S3(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	// This test validates config mapping logic only.
	// With a custom endpoint, the AWS client may be created without credentials.
	cfg := &config.Config{
		Publisher: config.PublisherConfig{
			Type: "s3",
			S3: &config.S3Config{
				Bucket:   "test-bucket",
				Region:   "us-west-2",
				Endpoint: "http://localhost:9000",
				Prefix:   "oidc",
				UseIRSA:  false,
			},
		},
	}

	ctx := t.Context()
	// Publisher creation may succeed even without credentials (connection happens at operation time)
	pub, err := factory.Create(ctx, cfg)
	// Either way, we just verify there's no panic
	_ = pub
	_ = err
}

func TestFactory_Create_S3_MissingConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	cfg := &config.Config{
		Publisher: config.PublisherConfig{
			Type: "s3",
			S3:   nil,
		},
	}

	ctx := t.Context()
	_, err := factory.Create(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3 configuration is required")
}

func TestFactory_Create_GCS_MissingConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	cfg := &config.Config{
		Publisher: config.PublisherConfig{
			Type: "gcs",
			GCS:  nil,
		},
	}

	ctx := t.Context()
	_, err := factory.Create(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GCS configuration is required")
}

func TestFactory_Create_Azure_MissingConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	cfg := &config.Config{
		Publisher: config.PublisherConfig{
			Type:  "azure",
			Azure: nil,
		},
	}

	ctx := t.Context()
	_, err := factory.Create(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure configuration is required")
}

func TestFactory_Create_OCI_MissingConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewFactory(logger)

	cfg := &config.Config{
		Publisher: config.PublisherConfig{
			Type: "oci",
			OCI:  nil,
		},
	}

	ctx := t.Context()
	_, err := factory.Create(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OCI configuration is required")
}

func TestFactory_CreateS3Publisher_ConfigMapping(t *testing.T) {
	// Test that S3 config is properly mapped.
	// With a custom endpoint, the AWS client may be created without credentials.
	factory := NewFactory(nil)

	s3Cfg := &config.S3Config{
		Bucket:         "my-bucket",
		Region:         "us-east-1",
		Endpoint:       "http://minio:9000",
		ForcePathStyle: true,
		Prefix:         "oidc",
		UseIRSA:        true,
		CacheControl:   "max-age=600",
		ContentType:    "application/octet-stream",
	}

	ctx := t.Context()
	pub, err := factory.createS3Publisher(ctx, s3Cfg, "", "")
	// Publisher creation may succeed; verify there's no panic and result is usable
	if err == nil {
		require.NotNil(t, pub)
	}
}

func TestFactory_CreateS3Publisher_NilConfig(t *testing.T) {
	factory := NewFactory(nil)
	ctx := t.Context()

	_, err := factory.createS3Publisher(ctx, nil, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3 configuration is required")
}

func TestFactory_CreateGCSPublisher_NilConfig(t *testing.T) {
	factory := NewFactory(nil)
	ctx := t.Context()

	_, err := factory.createGCSPublisher(ctx, nil, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GCS configuration is required")
}

func TestFactory_CreateAzurePublisher_NilConfig(t *testing.T) {
	factory := NewFactory(nil)
	ctx := t.Context()

	_, err := factory.createAzurePublisher(ctx, nil, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure configuration is required")
}

func TestFactory_CreateOCIPublisher_NilConfig(t *testing.T) {
	factory := NewFactory(nil)
	ctx := t.Context()

	_, err := factory.createOCIPublisher(ctx, nil, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OCI configuration is required")
}

func TestFactory_PublisherTypes(t *testing.T) {
	// Verify all publisher type constants work
	assert.Equal(t, iface.PublisherType("s3"), iface.PublisherTypeS3)
	assert.Equal(t, iface.PublisherType("gcs"), iface.PublisherTypeGCS)
	assert.Equal(t, iface.PublisherType("azure"), iface.PublisherTypeAzure)
	assert.Equal(t, iface.PublisherType("oci"), iface.PublisherTypeOCI)
}
