# KubeAssume Go Framework Usage Guide

## Table of Contents

- [Controller-Runtime](#controller-runtime)
- [Client-Go](#client-go)
- [Structured Logging (slog)](#structured-logging-slog)
- [Error Handling](#error-handling)
- [Prometheus Metrics](#prometheus-metrics)
- [Cobra CLI](#cobra-cli)
- [Cloud SDKs](#cloud-sdks)
- [Health Checks](#health-checks)

---

## Controller-Runtime

KubeAssume uses `sigs.k8s.io/controller-runtime` v0.18.x as the controller framework.

### Manager Setup

```go
import (
    ctrl "sigs.k8s.io/controller-runtime"
    metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

mgr, err := ctrl.NewManager(k8sCfg, ctrl.Options{
    Scheme: scheme,
    Metrics: metricsserver.Options{
        BindAddress: ":8080",
    },
    HealthProbeBindAddress: ":8081",
    LeaderElection:         true,
    LeaderElectionID:       "kubeassume-controller",
})
```

### Reconciler Pattern

KubeAssume uses a time-based reconciler (not watching resources). The reconciler requeues itself after `SyncPeriod`:

```go
func (r *OIDCBridgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Perform sync work...
    if err := r.sync(ctx); err != nil {
        return ctrl.Result{}, err // controller-runtime will requeue with backoff
    }
    return ctrl.Result{RequeueAfter: r.Config.SyncPeriod}, nil
}
```

### Controller Registration

```go
func (r *OIDCBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        Named("oidcbridge").
        Complete(r)
}
```

### Scheme Registration

```go
import (
    "k8s.io/apimachinery/pkg/runtime"
    utilruntime "k8s.io/apimachinery/pkg/util/runtime"
    clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var scheme = runtime.NewScheme()

func init() {
    utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}
```

### Health Probes

```go
import "sigs.k8s.io/controller-runtime/pkg/healthz"

mgr.AddHealthzCheck("healthz", healthz.Ping)
mgr.AddReadyzCheck("readyz", healthz.Ping)
```

---

## Client-Go

Used for direct Kubernetes API access beyond controller-runtime's client.

### REST Client (OIDC Bridge)

```go
import "k8s.io/client-go/rest"

restClient, err := rest.RESTClientFor(cfg.RESTConfig)
result := restClient.Get().AbsPath("/.well-known/openid-configuration").Do(ctx)
data, err := result.Raw()
```

### Typed Clientset (ConfigMap Store)

```go
import "k8s.io/client-go/kubernetes"

clientset, err := kubernetes.NewForConfig(k8sCfg)
cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
_, err = clientset.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
```

### K8s API Error Handling

```go
import "k8s.io/apimachinery/pkg/api/errors"

if errors.IsNotFound(err) {
    // Resource doesn't exist yet, create it
}
if errors.IsConflict(err) {
    // Optimistic locking conflict, retry
}
if errors.IsAlreadyExists(err) {
    // Resource already exists, update instead
}
```

### Event Recorder

```go
import "k8s.io/client-go/tools/record"

recorder := mgr.GetEventRecorderFor("kubeassume-controller")
recorder.Eventf(pod, corev1.EventTypeNormal, "Synced", "OIDC metadata synced successfully")
recorder.Eventf(pod, corev1.EventTypeWarning, "SyncFailed", "sync failed: %v", err)
recorder.Eventf(pod, corev1.EventTypeNormal, "KeyRotation", "New key detected: %s", keyID)
```

---

## Structured Logging (slog)

KubeAssume uses Go's built-in `log/slog` for structured logging, integrated with controller-runtime's zap backend.

### Logger Initialization

```go
import (
    "log/slog"
    ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// controller-runtime manages the global logger
ctrllog.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
logger := slog.Default()
```

### Logging Patterns

```go
// Debug: verbose operational details
logger.Debug("Fetching discovery document", "endpoint", url)

// Info: normal operational events
logger.Info("Successfully published OIDC metadata",
    "bucket", bucket,
    "key_count", len(keys),
)

// Warn: non-fatal issues
logger.Warn("Publisher validation failed (will retry)", "error", err)

// Error: failures requiring attention
logger.Error("Health check failed", "name", name, "error", err)
```

### Key Rules

- Never log tokens, keys, credentials, or secrets
- Always add structured key-value context
- Use consistent key names: `error`, `bucket`, `key_count`, `namespace`, `name`

---

## Error Handling

### Package: `pkg/errors`

KubeAssume defines domain-specific error types following K8s/CNCF patterns.

### Error Codes

```go
import kubeassumeerrors "github.com/salab/kube-iam-assume/pkg/errors"

// Sentinel errors for type checking
if kubeassumeerrors.IsConfigError(err) { ... }
if kubeassumeerrors.IsPublishError(err) { ... }
if kubeassumeerrors.IsRetryable(err) { ... }
```

### Error Wrapping

```go
// Always wrap with context using fmt.Errorf %w
return fmt.Errorf("failed to fetch discovery document: %w", err)

// Domain errors with codes
return kubeassumeerrors.NewPublishError("s3", "failed to upload JWKS", err)
return kubeassumeerrors.NewFetchError("bridge", "API server unreachable", err)
```

### Error Categories

| Category | Retryable | Example |
|----------|-----------|---------|
| `ErrConfig` | No | Invalid bucket name, missing region |
| `ErrFetch` | Yes | API server timeout, network error |
| `ErrPublish` | Yes | S3 upload failed, permission denied |
| `ErrRotation` | Yes | ConfigMap conflict, serialization error |
| `ErrValidation` | No | Invalid JWKS, missing required field |
| `ErrNotFound` | No | ConfigMap not found, bucket not found |

---

## Prometheus Metrics

### Registration Pattern

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

// promauto registers with default registry automatically
SyncTotal: promauto.NewCounterVec(
    prometheus.CounterOpts{
        Namespace: "kubeassume",
        Name:      "sync_total",
        Help:      "Total number of sync operations",
    },
    []string{"status"},
)
```

### Recording Metrics

```go
m.SyncTotal.WithLabelValues("success").Inc()
m.SyncDuration.WithLabelValues("fetch").Observe(seconds)
m.ActiveKeys.Set(float64(count))
m.LastPublishTimestamp.Set(float64(time.Now().Unix()))
```

### Custom Registry (for testing)

```go
reg := prometheus.NewRegistry()
m.Register(reg)
```

### Metric Naming Convention

```
kubeassume_<subsystem>_<metric>_<unit>
kubeassume_sync_total                     # Counter
kubeassume_sync_duration_seconds          # Histogram
kubeassume_active_keys                    # Gauge
kubeassume_last_publish_timestamp         # Gauge (unix)
```

---

## Cobra CLI

### Command Structure

```go
import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
    Use:   "kubeassume",
    Short: "KubeAssume enables secretless cloud access for Kubernetes",
}

// Subcommands
rootCmd.AddCommand(setupCmd)    // kubeassume setup
setupCmd.AddCommand(awsCmd)     // kubeassume setup aws
rootCmd.AddCommand(statusCmd)   // kubeassume status
rootCmd.AddCommand(versionCmd)  // kubeassume version
```

### Flag Binding

```go
cmd.Flags().StringVar(&issuerURL, "issuer-url", "", "Public OIDC issuer URL (required)")
cmd.MarkFlagRequired("issuer-url")

cmd.Flags().StringSliceVar(&audiences, "audiences", []string{"sts.amazonaws.com"}, "Allowed audiences")
cmd.Flags().DurationVar(&syncPeriod, "sync-period", 60*time.Second, "Sync interval")
```

### RunE Pattern

```go
var cmd = &cobra.Command{
    Use:  "status",
    RunE: func(cmd *cobra.Command, args []string) error {
        ctx := cmd.Context()
        // ... implementation
        return nil  // or error
    },
}
```

---

## Cloud SDKs

### AWS SDK v2

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/aws/aws-sdk-go-v2/service/iam"
)

// Load config (respects IRSA, env vars, instance profile)
cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))

// S3 client with custom endpoint (MinIO/LocalStack)
client := s3.NewFromConfig(cfg, func(o *s3.Options) {
    o.BaseEndpoint = aws.String(endpoint)
    o.UsePathStyle = true
})

// IAM client
iamClient := iam.NewFromConfig(cfg)
```

### Google Cloud Storage

```go
import (
    "cloud.google.com/go/storage"
    "google.golang.org/api/option"
)

// Uses Application Default Credentials (Workload Identity in GKE)
client, err := storage.NewClient(ctx, opts...)
bucket := client.Bucket(bucketName)
w := bucket.Object(objectName).NewWriter(ctx)
w.ContentType = "application/json"
w.CacheControl = "max-age=300"
```

### Azure Blob Storage

```go
import (
    "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
    "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// Uses DefaultAzureCredential (Managed Identity, CLI, env vars)
credential, err := azidentity.NewDefaultAzureCredential(nil)
client, err := azblob.NewClient(serviceURL, credential, nil)
_, err = client.UploadBuffer(ctx, container, blobName, data, &azblob.UploadBufferOptions{
    HTTPHeaders: &azblob.BlobHTTPHeaders{
        BlobContentType:  &contentType,
        BlobCacheControl: &cacheControl,
    },
})
```

### OCI Object Storage

```go
import (
    "github.com/oracle/oci-go-sdk/v65/common"
    "github.com/oracle/oci-go-sdk/v65/objectstorage"
)

// Instance Principal (recommended in OCI)
provider, err := common.AuthInstancePrincipalConfigurationProvider()
client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(provider)
client.SetRegion(region)

req := objectstorage.PutObjectRequest{
    NamespaceName: &namespace,
    BucketName:    &bucket,
    ObjectName:    &key,
    ContentType:   &contentType,
    PutObjectBody: io.NopCloser(bytes.NewReader(data)),
    ContentLength: &contentLength,
}
```

---

## Health Checks

### Registration Pattern

```go
h := health.New(logger)
h.Register("bridge", func(ctx context.Context) error {
    _, err := bridge.FetchDiscoveryDocument(ctx)
    return err
})
h.Register("publisher", func(ctx context.Context) error {
    return publisher.HealthCheck(ctx)
})
```

### Status Aggregation

```
StatusHealthy   -> all checks pass
StatusDegraded  -> some checks fail (non-critical)
StatusUnhealthy -> critical check fails
```

### Kubernetes Probes

```go
// Liveness: is the process alive?
LivenessCheck() -> always returns nil (process is running)

// Readiness: can we serve traffic?
ReadinessCheck() -> fails if any component is unhealthy
```

---

## Interface Patterns

### Publisher Interface

All publishers implement `publisher.Publisher`:
- `Publish(ctx, discovery, jwks) error`
- `Validate(ctx) error`
- `GetPublicURL() string`
- `HealthCheck(ctx) error`
- `Type() PublisherType`

Compile-time interface check:
```go
var _ publisher.Publisher = (*Publisher)(nil)
```

### Federation Provider Interface

All federation providers implement `federation.Provider`:
- `Setup(ctx, cfg) (*SetupResult, error)`
- `Validate(ctx, issuerURL) error`
- `GetProviderInfo(ctx, issuerURL) (*ProviderInfo, error)`
- `Delete(ctx, issuerURL) error`
- `Type() string`

### Store Interface (Rotation)

```go
type Store interface {
    Load(ctx context.Context) (*State, error)
    Save(ctx context.Context, state *State) error
}
```

Implementations: `ConfigMapStore` (Kubernetes ConfigMap backed).

---

## Build & LDFLAGS

```makefile
LDFLAGS := -ldflags="-w -s \
    -X main.Version=$(TAG) \
    -X main.GitCommit=$(shell git rev-parse --short HEAD)"

CGO_ENABLED=0 GOOS=linux go build $(LDFLAGS) -o bin/controller ./cmd/controller
```

- `-w -s`: strip debug info and symbol table
- `CGO_ENABLED=0`: static binary for distroless containers
