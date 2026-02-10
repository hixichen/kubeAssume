//go:build integration

// Package integration contains integration tests that require running infrastructure.
// Start MinIO first: cd local-test && docker compose up -d
// Then run: go test -tags=integration ./test/integration/...
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/hixichen/kube-iam-assume/pkg/bridge"
	"github.com/hixichen/kube-iam-assume/pkg/publisher/iface"
	s3pub "github.com/hixichen/kube-iam-assume/pkg/publisher/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	minioEndpoint = "http://localhost:9000"
	minioBucket   = "oidc"
	minioRegion   = "us-east-1"
)

func newMinioPublisher(t *testing.T, prefix string, multiCluster bool, clusterID string) iface.Publisher {
	t.Helper()
	cfg := s3pub.Config{
		Bucket:              minioBucket,
		Region:              minioRegion,
		Endpoint:            minioEndpoint,
		ForcePathStyle:      true,
		Prefix:              prefix,
		MultiClusterEnabled: multiCluster,
		ClusterID:           clusterID,
	}
	pub, err := s3pub.New(context.Background(), cfg, nil)
	require.NoError(t, err)
	return pub
}

func fetchJSON(t *testing.T, url string, dst interface{}) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx
	require.NoError(t, err, "GET %s", url)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s status", url)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, dst))
}

// TestMinIO_SingleCluster_Publish verifies that a single-cluster publish results
// in correct discovery and JWKS documents accessible via HTTP.
func TestMinIO_SingleCluster_Publish(t *testing.T) {
	ctx := context.Background()
	pub := newMinioPublisher(t, "single-cluster-test", false, "")

	discovery := &bridge.DiscoveryDocument{
		Issuer:                  pub.GetPublicURL(),
		JWKSURI:                 pub.GetPublicURL() + "/openid/v1/jwks",
		ResponseTypesSupported:  []string{"id_token"},
		SubjectTypesSupported:   []string{"public"},
		IDTokenSigningAlgValues: []string{"RS256"},
	}
	jwks := &bridge.JWKS{
		Keys: []bridge.JWK{
			{Kid: "test-key-1", Kty: "RSA", N: "abc123", E: "AQAB"},
		},
	}

	require.NoError(t, pub.Publish(ctx, discovery, jwks))

	// Verify discovery document is publicly accessible and issuer matches
	var gotDiscovery bridge.DiscoveryDocument
	fetchJSON(t, pub.GetPublicURL()+"/.well-known/openid-configuration", &gotDiscovery)
	assert.Equal(t, pub.GetPublicURL(), gotDiscovery.Issuer, "issuer must match public URL")

	// Verify JWKS is publicly accessible
	var gotJWKS bridge.JWKS
	fetchJSON(t, pub.GetPublicURL()+"/openid/v1/jwks", &gotJWKS)
	require.Len(t, gotJWKS.Keys, 1)
	assert.Equal(t, "test-key-1", gotJWKS.Keys[0].Kid)
}

// TestMinIO_MultiCluster_Aggregation simulates three clusters writing per-cluster JWKS
// and verifies the aggregated JWKS at the root path contains all unique keys.
func TestMinIO_MultiCluster_Aggregation(t *testing.T) {
	ctx := context.Background()

	// Use a unique group per test run to avoid cross-test interference
	clusterGroup := fmt.Sprintf("prod-test-%d", time.Now().UnixNano())

	clusters := []struct {
		id  string
		kid string
	}{
		{"cluster-a", "key-a"},
		{"cluster-b", "key-b"},
		{"cluster-c", "key-c"},
	}

	// Each cluster publishes its own per-cluster JWKS
	for _, cl := range clusters {
		pub := newMinioPublisher(t, clusterGroup, true, cl.id)
		discovery := &bridge.DiscoveryDocument{
			Issuer:                  pub.GetPublicURL(),
			JWKSURI:                 pub.GetPublicURL() + "/openid/v1/jwks",
			ResponseTypesSupported:  []string{"id_token"},
			SubjectTypesSupported:   []string{"public"},
			IDTokenSigningAlgValues: []string{"RS256"},
		}
		jwks := &bridge.JWKS{
			Keys: []bridge.JWK{
				{Kid: cl.kid, Kty: "RSA", N: "abc", E: "AQAB"},
			},
		}
		require.NoError(t, pub.Publish(ctx, discovery, jwks), "cluster %s", cl.id)
	}

	// Use the aggregator interface to list all cluster JWKS and publish merged result
	aggPub := newMinioPublisher(t, clusterGroup, true, clusters[0].id)
	agg, ok := aggPub.(iface.MultiClusterAggregator)
	require.True(t, ok, "S3 publisher must implement MultiClusterAggregator")

	clusterJWKS, err := agg.ListClusterJWKS(ctx)
	require.NoError(t, err)
	assert.Len(t, clusterJWKS, len(clusters))

	// Merge all keys (dedup by kid)
	seen := make(map[string]struct{})
	merged := &bridge.JWKS{}
	for _, j := range clusterJWKS {
		for _, k := range j.Keys {
			if _, exists := seen[k.Kid]; !exists {
				seen[k.Kid] = struct{}{}
				merged.Keys = append(merged.Keys, k)
			}
		}
	}
	require.NoError(t, agg.PublishAggregatedJWKS(ctx, merged))

	// Verify aggregated JWKS is reachable and has all 3 keys
	aggURL := fmt.Sprintf("%s/%s/%s/openid/v1/jwks", minioEndpoint, minioBucket, clusterGroup)
	var gotJWKS bridge.JWKS
	fetchJSON(t, aggURL, &gotJWKS)
	assert.Len(t, gotJWKS.Keys, len(clusters))

	kids := make(map[string]struct{})
	for _, k := range gotJWKS.Keys {
		kids[k.Kid] = struct{}{}
	}
	for _, cl := range clusters {
		assert.Contains(t, kids, cl.kid)
	}
}
