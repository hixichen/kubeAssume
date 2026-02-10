# Local Testing Guide

## Prerequisites

- Docker Desktop (or compatible runtime)
- [kind](https://kind.sigs.k8s.io/) v0.20+
- [kubectl](https://kubernetes.io/docs/tasks/tools/) v1.28+
- [helm](https://helm.sh/docs/intro/install/) v3.15+
- Go 1.22+

## Quick Start

```bash
# 1. Start local infra (MinIO + kind cluster)
./setup.sh

# 2. Build and load controller image
./build-and-load.sh

# 3. Deploy controller
./deploy.sh

# 4. Verify OIDC endpoints
./verify.sh

# 5. Teardown
./teardown.sh
```

## Architecture

```
┌─────────────────────────────────────────┐
│            kind cluster                  │
│  ┌──────────────────────────────┐       │
│  │  kubeassume controller pod    │       │
│  │  (fetches OIDC from API)      │──┐   │
│  └──────────────────────────────┘  │   │
│                                     │   │
└─────────────────────────────────────│───┘
                                      │
                                      ▼
                         ┌─────────────────┐
                         │   MinIO (S3)     │
                         │  localhost:9000  │
                         │  bucket: oidc    │
                         └─────────────────┘
```

## Test Scenarios

### 1. Basic Sync

Verify the controller fetches OIDC metadata from the kind API server and publishes to MinIO.

```bash
./verify.sh
```

### 2. Key Rotation

Simulate key rotation by restarting the API server with new signing keys:

```bash
./test-rotation.sh
```

### 3. Health Endpoints

```bash
# Liveness
kubectl port-forward -n kubeassume-system deploy/kubeassume-controller 8081:8081 &
curl http://localhost:8081/healthz

# Metrics
kubectl port-forward -n kubeassume-system deploy/kubeassume-controller 8080:8080 &
curl http://localhost:8080/metrics | grep kubeassume_
```

## Manual Testing (without kind)

Run the controller locally against an existing cluster:

```bash
# 1. Start local infra (MinIO)
docker compose -f local-test/docker-compose.yml up -d

# 2. Create a config.yaml file (e.g., config.yaml)
cat <<EOF > config.yaml
controller:
  syncPeriod: "30s"
  rotationOverlap: "24h"
publisher:
  type: "s3"
  s3:
    bucket: "oidc"
    region: "us-east-1"
    endpoint: "http://localhost:9000"
    forcePathStyle: true
    useIRSA: false # MinIO typically doesn't use IRSA
EOF

# 3. Run controller binary with the config file
go run ./cmd/controller --config=config.yaml
```

## Verifying the Hybrid Model (Leader Election for Polling)

To verify the new hybrid model, you can scale up the number of controller replicas and observe their logs.

1.  **Scale up the deployment:**
    ```bash
    kubectl scale deployment -n kube-iam-assume-system kube-iam-assume --replicas=3
    ```

2.  **Find the leader:**
    The leader is recorded in the lease object.
    ```bash
    kubectl get lease -n kube-iam-assume-system kube-iam-assume-controller-leader-election -o yaml
    ```
    Look for the `holderIdentity`. It will be the name of the leader pod.

3.  **Check the leader's logs:**
    Replace `<leader-pod-name>` with the `holderIdentity` from the previous step.
    ```bash
    kubectl logs -n kube-iam-assume-system -f <leader-pod-name>
    ```
    You should see logs like:
    ```
    "Starting OIDC poller"
    "Polling for OIDC metadata"
    "Created OIDC metadata ConfigMap" or "Updated OIDC metadata ConfigMap"
    "Reconciliation triggered by OIDC metadata ConfigMap change"
    "Uploaded object to S3"
    ```

4.  **Check a follower's logs:**
    Get the name of a non-leader pod and check its logs.
    ```bash
    kubectl logs -n kube-iam-assume-system -f <follower-pod-name>
    ```
    You should **not** see "Starting OIDC poller" or "Polling for OIDC metadata". You *should* see reconciliation logs triggered by the ConfigMap change, and you may see logs about publishing being skipped because another replica already did it.
    ```
    "Reconciliation triggered by OIDC metadata ConfigMap change"
    "Object was updated by another replica, skipping update"
    ```

This confirms that only the leader is polling the Kubernetes API, while all replicas are ready to publish, with optimistic locking preventing race conditions.

## Cleanup

```bash
./teardown.sh
```
