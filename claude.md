# KubeAssume v0.1 Implementation Plan

## Overview

KubeAssume is a Kubernetes controller that enables secretless cloud access for self-hosted Kubernetes clusters by publishing OIDC discovery metadata publicly, bridging the gap between K8s service account tokens and cloud provider identity federation.

## Architecture

The controller uses a hybrid model for high availability and efficiency:
-   **Leader Election for Polling:** A single controller instance is elected as the leader. This leader is solely responsible for periodically polling the Kubernetes API server's `/openid/v1/jwks` endpoint to detect changes, such as key rotations.
-   **ConfigMap as a Cache:** The leader writes the fetched OIDC metadata into a shared `kube-iam-assume-oidc-metadata` ConfigMap within the cluster.
-   **Decentralized Publishing:** All controller instances (leader and followers) watch this ConfigMap. When the ConfigMap is updated, all instances are notified.
-   **Optimistic Concurrency:** Each instance then independently attempts to publish the new metadata to the configured cloud storage backend (S3 or GCS). The publish operation uses optimistic locking (ETags for S3, Generation numbers for GCS) to ensure that only one write succeeds per update, preventing race conditions.

This design minimizes load on the Kubernetes API server while ensuring high availability for the critical publishing step.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            KubeAssume Controller (Leader)                     │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐    ┌──────────────────────────┐                          │
│  │    Bridge    │───▶│  Writes OIDC Metadata    │                          │
│  │  (K8s OIDC)  │    │  to ConfigMap            │                          │
│  └──────────────┘    └──────────────────────────┘                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
       │
       │ Updates
       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       kube-iam-assume-oidc-metadata ConfigMap                 │
└─────────────────────────────────────────────────────────────────────────────┘
       │
       │ Watched by all controller instances
       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│               KubeAssume Controller (All Instances - Leader & Followers)      │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────────┐    ┌──────────────────────────────┐                   │
│  │  Reads from      │───▶│         Publisher            │                   │
│  │  ConfigMap Watch │    │  (Optimistic Locking)        │                   │
│  └──────────────────┘    │  ┌────┐ ┌────┐              │                   │
│                        │  │ S3 │ │GCS │              │                   │
│                        │  └────┘ └────┘              │                   │
│                        └──────────────────────────────┘                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Project Structure

```
kubeassume/
├── cmd/
│   ├── controller/main.go          # Controller entrypoint
│   └── cli/
│       ├── main.go                 # CLI entrypoint
│       └── commands/               # CLI commands
│           ├── setup/              # Federation setup commands
│           │   ├── setup.go        # Parent setup command
│           │   ├── aws.go          # AWS IAM OIDC provider
│           │   ├── gcp.go          # GCP Workload Identity Federation
│           │   ├── azure.go        # Azure AD federated credentials
│           │   └── oci.go          # OCI identity federation
│           └── status.go           # Status command
├── pkg/
│   ├── bridge/                     # OIDC discovery + JWKS fetching
│   │   ├── bridge.go
│   │   ├── discovery.go
│   │   ├── jwks.go
│   │   └── types.go
│   ├── publisher/                  # Publishing backends (interface + implementations)
│   │   ├── publisher.go            # Interface
│   │   ├── factory.go              # Publisher factory
│   │   ├── s3/                     # AWS S3
│   │   │   ├── s3.go
│   │   │   └── config.go
│   │   ├── gcs/                    # Google Cloud Storage
│   │   │   ├── gcs.go
│   │   │   └── config.go
│   │   ├── azure/                  # Azure Blob Storage
│   │   │   ├── azure.go
│   │   │   └── config.go
│   │   └── oci/                    # OCI Object Storage
│   │       ├── oci.go
│   │       └── config.go
│   ├── federation/                 # Cloud identity federation setup
│   │   ├── federation.go           # Interface
│   │   ├── aws/                    # AWS IAM OIDC Provider
│   │   │   └── aws.go
│   │   ├── gcp/                    # GCP Workload Identity Federation
│   │   │   └── gcp.go
│   │   ├── azure/                  # Azure AD Federated Credentials
│   │   │   └── azure.go
│   │   └── oci/                    # OCI Identity Federation
│   │       └── oci.go
│   ├── rotation/                   # Key rotation detection
│   │   ├── rotation.go
│   │   ├── types.go
│   │   ├── store.go
│   │   └── merger.go
│   ├── health/                     # Health checks
│   │   └── health.go
│   └── metrics/                    # Prometheus metrics
│       └── metrics.go
├── internal/controller/            # Controller reconciliation
│   └── oidcbridge_controller.go
├── deploy/helm/kubeassume/         # Helm chart
├── hack/                           # Dev scripts
├── test/
│   ├── unit/
│   ├── integration/
│   └── e2e/
├── Makefile
├── Dockerfile
├── go.mod
└── claude.md                       # This file
```

## Core Dependencies

| Dependency | Purpose | Version |
|------------|---------|---------|
| `sigs.k8s.io/controller-runtime` | Controller framework | v0.18.x |
| `k8s.io/client-go` | K8s API client | v0.30.x |
| `github.com/aws/aws-sdk-go-v2` | AWS S3/IAM operations | Latest |
| `cloud.google.com/go/storage` | GCS operations | Latest |
| `cloud.google.com/go/iam` | GCP IAM operations | Latest |
| `github.com/Azure/azure-sdk-for-go` | Azure Blob/AD operations | Latest |
| `github.com/oracle/oci-go-sdk` | OCI operations | Latest |
| `github.com/spf13/cobra` | CLI framework | v1.8.x |
| `github.com/prometheus/client_golang` | Metrics | v1.19.x |

## Component Interfaces

### Publisher Interface (pkg/publisher/publisher.go)

```go
type Publisher interface {
    // Publish uploads discovery document and JWKS
    Publish(ctx context.Context, discovery *bridge.DiscoveryDocument, jwks *bridge.JWKS) error
    // Validate checks configuration and permissions
    Validate(ctx context.Context) error
    // GetPublicURL returns the public issuer URL
    GetPublicURL() string
    // HealthCheck verifies backend accessibility
    HealthCheck(ctx context.Context) error
    // Type returns the publisher type (s3, gcs, azure, oci)
    Type() string
}
```

### Federation Interface (pkg/federation/federation.go)

```go
type Provider interface {
    // Setup creates the OIDC identity provider/federation
    Setup(ctx context.Context, cfg SetupConfig) (*SetupResult, error)
    // Validate checks if setup is valid
    Validate(ctx context.Context, issuerURL string) error
    // GetProviderInfo returns info about existing provider
    GetProviderInfo(ctx context.Context, issuerURL string) (*ProviderInfo, error)
    // Delete removes the OIDC provider
    Delete(ctx context.Context, issuerURL string) error
    // Type returns the provider type (aws, gcp, azure, oci)
    Type() string
}

type SetupConfig struct {
    IssuerURL  string
    Audiences  []string
    // Provider-specific options
    Options    map[string]interface{}
}

type SetupResult struct {
    ProviderARN  string   // AWS: arn:aws:iam::..., GCP: projects/..., etc.
    Audiences    []string
    Thumbprint   string
}
```

## Cloud Provider Details

### Publishing Backends

| Provider | Service | Public URL Format |
|----------|---------|-------------------|
| AWS | S3 | `https://BUCKET.s3.REGION.amazonaws.com` |
| GCP | GCS | `https://storage.googleapis.com/BUCKET` |
| Azure | Blob | `https://ACCOUNT.blob.core.windows.net/CONTAINER` |
| OCI | Object Storage | `https://objectstorage.REGION.oraclecloud.com/n/NAMESPACE/b/BUCKET/o` |

### Federation Setup

| Provider | Service | Result |
|----------|---------|--------|
| AWS | IAM OIDC Provider | `arn:aws:iam::ACCOUNT:oidc-provider/ISSUER` |
| GCP | Workload Identity Pool | `projects/PROJECT/locations/global/workloadIdentityPools/POOL` |
| Azure | Federated Credentials | App Registration with federated credential |
| OCI | Identity Domain | OIDC Identity Provider in Identity Domain |

## CLI Commands

```bash
# Publishing is handled by controller, CLI handles federation setup

# AWS
kubeassume setup aws --issuer-url https://... --region us-west-2

# GCP
kubeassume setup gcp --issuer-url https://... --project my-project --pool-id my-pool

# Azure
kubeassume setup azure --issuer-url https://... --tenant-id ... --app-id ...

# OCI
kubeassume setup oci --issuer-url https://... --compartment-id ...

# Status (shows all configured federations)
kubeassume status
```

## Helm Values Structure

```yaml
# Publisher configuration (pick one)
publisher:
  type: s3  # s3, gcs, azure, oci

  # AWS S3
  s3:
    bucket: ""
    region: ""
    useIRSA: true

  # Google Cloud Storage
  gcs:
    bucket: ""
    project: ""
    useWorkloadIdentity: true

  # Azure Blob Storage
  azure:
    storageAccount: ""
    container: ""
    useManagedIdentity: true

  # OCI Object Storage
  oci:
    bucket: ""
    namespace: ""
    compartmentId: ""

controller:
  syncPeriod: 60s
  rotation:
    overlapPeriod: 24h
```

## Implementation Phases

### Phase 1: Foundation (Current)
- [x] Project structure
- [x] go.mod with dependencies
- [x] pkg/bridge - OIDC fetching
- [x] pkg/publisher interface + S3 implementation
- [x] pkg/rotation - key rotation
- [x] pkg/health, pkg/metrics
- [x] internal/controller
- [x] Helm chart
- [x] Dockerfile, Makefile

### Phase 2: Multi-Cloud Publishing
- [ ] pkg/publisher/gcs - GCS implementation
- [ ] pkg/publisher/azure - Azure Blob implementation
- [ ] pkg/publisher/oci - OCI Object Storage implementation
- [ ] pkg/publisher/factory - Publisher factory

### Phase 3: Federation Setup
- [ ] pkg/federation interface
- [ ] pkg/federation/aws - AWS IAM OIDC Provider
- [ ] pkg/federation/gcp - GCP Workload Identity
- [ ] pkg/federation/azure - Azure AD Federation
- [ ] pkg/federation/oci - OCI Identity Federation
- [ ] CLI commands for each provider

### Phase 4: Testing & Polish
- [ ] Unit tests (85%+ coverage)
- [ ] Integration tests with minio/fake-gcs
- [ ] E2E tests with kind
- [ ] Documentation
- [ ] CNCF compliance docs

## Security Considerations

### Pod Security
- `runAsNonRoot: true`
- `readOnlyRootFilesystem: true`
- `allowPrivilegeEscalation: false`
- `capabilities: drop: [ALL]`

### Credentials
- AWS: IRSA (recommended), Instance Profile, or explicit credentials
- GCP: Workload Identity (recommended) or Service Account key
- Azure: Managed Identity (recommended) or Service Principal
- OCI: Instance Principal (recommended) or API key

### Bucket/Container Policies
All storage backends need public read access for OIDC paths only:
- `/.well-known/openid-configuration`
- `/openid/v1/jwks`

## Metrics

```
kubeassume_sync_total{status="success|error", publisher="s3|gcs|azure|oci"}
kubeassume_sync_duration_seconds{phase="fetch|publish"}
kubeassume_rotation_total{type="new_key|key_expired"}
kubeassume_active_keys
kubeassume_publish_errors_total{publisher="s3|gcs|azure|oci"}
kubeassume_last_publish_timestamp
```
