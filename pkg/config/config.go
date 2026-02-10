// Package config provides configuration management for the application
package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

// dnsLabelRe validates that a string is safe for use as a path component and DNS label.
var dnsLabelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

// Config holds the application configuration.
type Config struct {
	Controller ControllerConfig `mapstructure:"controller"`
	Publisher  PublisherConfig  `mapstructure:"publisher"`
}

// ControllerConfig holds controller-specific configuration.
type ControllerConfig struct {
	SyncPeriod      string               `mapstructure:"syncPeriod"`
	RotationOverlap string               `mapstructure:"rotationOverlap"`
	LeaderElection  LeaderElectionConfig `mapstructure:"leaderElection"`

	// ClusterGroup enables multi-cluster shared issuer mode.
	// All clusters with the same clusterGroup share one issuer URL and one aggregated JWKS endpoint.
	// When set, this value is used as the storage prefix. Empty = single-cluster mode (default).
	ClusterGroup string `mapstructure:"clusterGroup"`

	// ClusterID is the unique identifier for this cluster within the group.
	// Required when ClusterGroup is set. Must match ^[a-z0-9][a-z0-9-]*[a-z0-9]$.
	ClusterID string `mapstructure:"clusterID"`

	// AggregationInterval is how often the leader aggregates cluster JWKS (default: "5m")
	AggregationInterval string `mapstructure:"aggregationInterval"`

	// ClusterTTL is how long to keep a cluster's keys after its last update (default: "48h")
	ClusterTTL string `mapstructure:"clusterTTL"`
}

// LeaderElectionConfig holds leader election configuration.
type LeaderElectionConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	ID      string `mapstructure:"id"`
}

// PublisherConfig holds publisher configuration.
type PublisherConfig struct {
	Type  string       `mapstructure:"type"`
	S3    *S3Config    `mapstructure:"s3,omitempty"`
	GCS   *GCSConfig   `mapstructure:"gcs,omitempty"`
	Azure *AzureConfig `mapstructure:"azure,omitempty"`
	OCI   *OCIConfig   `mapstructure:"oci,omitempty"`
}

// AzureConfig holds Azure Blob Storage publisher configuration.
type AzureConfig struct {
	StorageAccount     string `mapstructure:"storageAccount"`
	Container          string `mapstructure:"container"`
	Prefix             string `mapstructure:"prefix,omitempty"`
	UseManagedIdentity bool   `mapstructure:"useManagedIdentity,omitempty"`
	TenantID           string `mapstructure:"tenantId,omitempty"`
	ClientID           string `mapstructure:"clientId,omitempty"`
	ClientSecret       string `mapstructure:"clientSecret,omitempty"`
	CacheControl       string `mapstructure:"cacheControl,omitempty"`
	ContentType        string `mapstructure:"contentType,omitempty"`
}

// OCIConfig holds OCI Object Storage publisher configuration.
type OCIConfig struct {
	Bucket               string `mapstructure:"bucket"`
	Namespace            string `mapstructure:"namespace"`
	Region               string `mapstructure:"region,omitempty"`
	Prefix               string `mapstructure:"prefix,omitempty"`
	UseInstancePrincipal bool   `mapstructure:"useInstancePrincipal,omitempty"`
	UserID               string `mapstructure:"userId,omitempty"`
	Fingerprint          string `mapstructure:"fingerprint,omitempty"`
	KeyFile              string `mapstructure:"keyFile,omitempty"`
	TenancyID            string `mapstructure:"tenancyId,omitempty"`
	CacheControl         string `mapstructure:"cacheControl,omitempty"`
	ContentType          string `mapstructure:"contentType,omitempty"`
}

// S3Config holds S3 publisher configuration.
type S3Config struct {
	Bucket         string `mapstructure:"bucket"`
	Region         string `mapstructure:"region"`
	Endpoint       string `mapstructure:"endpoint,omitempty"`
	ForcePathStyle bool   `mapstructure:"forcePathStyle,omitempty"`
	Prefix         string `mapstructure:"prefix,omitempty"`
	UseIRSA        bool   `mapstructure:"useIRSA,omitempty"`
	CacheControl   string `mapstructure:"cacheControl,omitempty"`
	ContentType    string `mapstructure:"contentType,omitempty"`
}

// GCSConfig holds GCS publisher configuration.
type GCSConfig struct {
	Bucket              string `mapstructure:"bucket"`
	Project             string `mapstructure:"project"`
	Prefix              string `mapstructure:"prefix,omitempty"`
	UseWorkloadIdentity bool   `mapstructure:"useWorkloadIdentity,omitempty"`
	CacheControl        string `mapstructure:"cacheControl,omitempty"`
	ContentType         string `mapstructure:"contentType,omitempty"`
}

// LoadConfig loads the configuration from a file.
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := config.Controller.validate(); err != nil {
		return nil, fmt.Errorf("invalid controller config: %w", err)
	}

	return &config, nil
}

// validate validates ControllerConfig fields.
func (c *ControllerConfig) validate() error {
	if c.ClusterGroup == "" {
		return nil // single-cluster mode, no further checks needed
	}
	if !dnsLabelRe.MatchString(c.ClusterGroup) {
		return fmt.Errorf("clusterGroup %q must match ^[a-z0-9][a-z0-9-]*[a-z0-9]$", c.ClusterGroup)
	}
	if c.ClusterID == "" {
		return fmt.Errorf("clusterID is required when clusterGroup is set")
	}
	if !dnsLabelRe.MatchString(c.ClusterID) {
		return fmt.Errorf("clusterID %q must match ^[a-z0-9][a-z0-9-]*[a-z0-9]$", c.ClusterID)
	}
	return nil
}
