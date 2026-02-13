# kube-iam-assume — Architecture

This document covers the internal design, operational details, and configuration reference. For a project overview and quick start, see [README.md](README.md).

---

## Table of Contents

- [How It Works](#how-it-works)
- [Token Exchange Flow](#token-exchange-flow)
- [Internal Design](#internal-design)
- [Key Rotation](#key-rotation)
- [Multi-Cluster Shared Issuer](#multi-cluster-shared-issuer)
- [Vault Integration](#vault-integration)
- [Publishing Modes](#publishing-modes)
- [Issuer Configuration](#issuer-configuration)
- [Distribution-Specific Guidance](#distribution-specific-guidance)
- [Bucket Naming](#bucket-naming)
- [Security Model](#security-model)
- [Roadmap](#roadmap)
- [FAQ](#faq)

---

## How It Works

kube-iam-assume is a single Kubernetes controller that:

1. Fetches the cluster's OIDC discovery document and JWKS from the API server (in-cluster, authenticated).
2. Publishes them to a publicly readable object storage bucket (S3, GCS, or Azure Blob).
3. Monitors for signing key rotation and handles it with zero-downtime dual-key publishing.

Once running, the cluster's OIDC endpoint is reachable from the public internet, which is all cloud IAM services require to validate Kubernetes service account tokens.

---

## Token Exchange Flow

When a workload presents a Kubernetes service account token to a cloud provider (e.g., AWS STS), the cloud provider performs this sequence:

1. Extract the `iss` (issuer) claim from the JWT.
2. Fetch `<issuer>/.well-known/openid-configuration`.
3. Extract `jwks_uri` from the discovery document.
4. Fetch the JWKS (public keys) from that URI.
5. Validate the JWT signature against the public keys.
6. Check `sub` and `aud` claims match the cloud IAM trust policy conditions.
7. Return temporary cloud credentials.

kube-iam-assume makes steps 2–4 possible for self-hosted clusters by publishing the discovery document and JWKS at a publicly reachable URL.

```
                                Internet
                                   |
                      +------------+------------+
                      |                         |
                Cloud Provider STS         Object Storage
                (AWS/GCP/Azure)            (S3/GCS/Blob)
                      |                         ^
                      | 2. Fetch JWKS           | 1. Publish JWKS
                      |    from public URL      |    on change
                      v                         |
                +-----+------+           +------+-------+
                | Validates  |           | kube-iam-assume   |
                | JWT sig    |           | Controller   |
                | + claims   |           | (in-cluster) |
                +-----+------+           +------+-------+
                      |                         |
                      | 3. Returns              | Polls /openid/v1/jwks
                      |    temp creds           | every 60s
                      v                         v
                +-----+------+           +------+-------+
                | Cloud SDK  |           | K8s API      |
                | in Pod     |           | Server       |
                +------------+           +--------------+
```

---

## Internal Design

The controller uses a hybrid leader-follower model for high availability and efficiency.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      KubeAssume Controller (Leader)                          │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐    ┌──────────────────────────┐                          │
│  │    Bridge    │───▶│  Writes OIDC Metadata    │                          │
│  │  (K8s OIDC)  │    │  to ConfigMap            │                          │
│  └──────────────┘    └──────────────────────────┘                          │
└─────────────────────────────────────────────────────────────────────────────┘
       │ Updates
       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│               kube-iam-assume-oidc-metadata ConfigMap                        │
└─────────────────────────────────────────────────────────────────────────────┘
       │ Watched by all instances
       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│           KubeAssume Controller (All Instances — Leader & Followers)         │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────────┐    ┌──────────────────────────────┐                   │
│  │  Reads from      │───▶│         Publisher            │                   │
│  │  ConfigMap Watch │    │  (Optimistic Locking)        │                   │
│  └──────────────────┘    │  ┌────┐ ┌────┐              │                   │
│                           │  │ S3 │ │GCS │              │                   │
│                           │  └────┘ └────┘              │                   │
│                           └──────────────────────────────┘                   │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Leader Election for Polling:** A single elected leader periodically polls the API server's `/openid/v1/jwks` endpoint to detect changes such as key rotations.

**ConfigMap as Cache:** The leader writes the fetched OIDC metadata into a shared `kube-iam-assume-oidc-metadata` ConfigMap.

**Decentralized Publishing:** All instances (leader and followers) watch the ConfigMap. When it is updated, all instances are notified and independently attempt to publish to the configured backend.

**Optimistic Concurrency:** Publish operations use optimistic locking (ETags for S3, generation numbers for GCS) to ensure exactly one write succeeds per ConfigMap update. This prevents race conditions without requiring inter-pod coordination.

This design minimises load on the Kubernetes API server while ensuring high availability for the critical publishing step.

### Components

| Component | Status | Description |
|---|---|---|
| OIDC Bridge Controller | v0.1 | Publishes and syncs JWKS to public endpoint, handles rotation |
| CLI (`kube-iam-assume`) | v0.1 | Cloud provider OIDC IdP registration and diagnostics |
| Helm Chart | v0.1 | Single-command installation |
| Prometheus Metrics | v0.2 | Sync count, rotation count, publish latency, errors |
| Terraform Modules | v0.2 | Cloud-side OIDC IdP registration for AWS, GCP, Azure |
| `CloudIdentityBinding` CRD | v0.3 | Declarative K8s SA to cloud identity mapping |
| Mutating Webhook | v0.3 | Auto-injects projected volume and cloud env vars into pods |

---

## Key Rotation

kube-iam-assume handles signing key rotation automatically.

The controller polls `/openid/v1/jwks` every 60 seconds (configurable). When a key change is detected:

1. The new key set is merged with the previous key set.
2. The merged JWKS (containing both old and new keys) is published.
3. After the overlap period (default: 24 hours), the old keys are removed.

During the overlap window, tokens signed by either key set are valid. This is the same strategy EKS uses.

| State | Published JWKS | Valid Tokens | Duration |
|---|---|---|---|
| Steady | [Key A] | Signed by Key A | Indefinite |
| Rotation detected | [Key A, Key B] | Signed by A or B | Overlap period (default 24h) |
| Overlap expired | [Key B] | Signed by Key B | Indefinite |

The controller emits Kubernetes Events on every rotation and exposes Prometheus metrics.

---

## Multi-Cluster Shared Issuer

> **Default behaviour is unchanged.** Each cluster gets its own issuer URL. This section describes an opt-in feature for environments that run multiple clusters and want workloads to move across them without YAML changes.

### The Problem It Solves

In single-cluster mode the OIDC issuer URL is tied to that cluster's bucket. If a workload moves to a different cluster (blue/green, region failover, scale-out), the new cluster has a different issuer URL and the same IAM trust policy no longer matches.

The multi-cluster shared issuer feature solves this: all clusters in the same group share one issuer URL, one JWKS endpoint, and therefore one set of IAM trust policies.

### Why It Works

Every Kubernetes cluster generates its own RSA/ECDSA key pair. Each key has a unique `kid` (derived from the SHA-256 hash of the public key). No two clusters share the same `kid`.

When any cluster in the group presents a token to AWS STS:

1. Token `iss` = shared group issuer URL → matches the IAM trust policy
2. Token `kid` identifies which cluster signed it → AWS fetches the aggregated JWKS and finds the right key
3. Token `sub` = `system:serviceaccount:<namespace>:<name>` → identical for the same SA across all clusters

Same YAML, same IAM role annotation, works on every cluster in the group.

### Configuration

```yaml
# Single-cluster mode (default, no changes needed)
controller:
  syncPeriod: 60s

publisher:
  type: s3
  s3:
    bucket: my-cluster-oidc
    region: us-west-2
# Issuer URL: https://my-cluster-oidc.s3.us-west-2.amazonaws.com
```

```yaml
# Multi-cluster mode — Cluster A
controller:
  clusterGroup: prod           # groups clusters sharing one issuer URL
  clusterID: prod-us-west-2   # unique name within the group

publisher:
  type: s3
  s3:
    bucket: my-company-oidc   # shared bucket for the whole group
    region: us-west-2
# Issuer URL: https://my-company-oidc.s3.us-west-2.amazonaws.com/prod
```

```yaml
# Multi-cluster mode — Cluster B (same group, different ID)
controller:
  clusterGroup: prod
  clusterID: prod-eu-west-1

publisher:
  type: s3
  s3:
    bucket: my-company-oidc
    region: us-west-2
# Issuer URL: https://my-company-oidc.s3.us-west-2.amazonaws.com/prod  (identical)
```

### Storage Layout

```
s3://my-company-oidc/
  prod/                                      ← clusterGroup = "prod"
    .well-known/openid-configuration         ← issuer = .../prod (all clusters identical)
    openid/v1/jwks                           ← aggregated: union of all prod cluster keys
    clusters/
      prod-us-west-2/openid/v1/jwks          ← written by cluster A only
      prod-eu-west-1/openid/v1/jwks          ← written by cluster B only
  staging/                                   ← clusterGroup = "staging", fully isolated
    .well-known/openid-configuration
    openid/v1/jwks
    clusters/
      staging-us-east-1/openid/v1/jwks
```

Each cluster writes only to its own sub-path. The aggregated root JWKS is written by the elected leader across all clusters in the group on a configurable interval (default: 5 minutes).

### Multi-Cluster Configuration Reference

| Field | Default | Description |
|---|---|---|
| `controller.clusterGroup` | `""` | Group name; empty disables multi-cluster mode |
| `controller.clusterID` | `""` | Unique cluster ID within the group; required when `clusterGroup` is set |
| `controller.aggregationInterval` | `"5m"` | How often the leader aggregates all cluster JWKS |
| `controller.clusterTTL` | `"48h"` | Exclude clusters from aggregation after this idle duration |

`clusterGroup` and `clusterID` must match `^[a-z0-9][a-z0-9-]*[a-z0-9]$`.

### Cluster Decommissioning

When a cluster is permanently removed, its per-cluster JWKS sub-path becomes stale. The `clusterTTL` (default: 48 hours) controls how long the leader waits before dropping a cluster from aggregation if it has not published an update. To decommission immediately, delete the `clusters/<clusterID>/` sub-path in the bucket.

---

## Vault Integration

The target scenario for this section is **external Vault**: HCP Vault, Vault deployed on a cloud VM, or a centralised Vault cluster serving multiple Kubernetes clusters. External Vault has no network path to your cluster's API server and therefore cannot use the Kubernetes auth method's TokenReview mechanism. It can only validate tokens by fetching a publicly reachable JWKS — which is exactly what kube-iam-assume publishes.

If Vault is running inside the same cluster and can reach the API server directly, you would use the Kubernetes auth method (`auth/kubernetes`) and kube-iam-assume is not involved.

### How Token Validation Works

External Vault uses the JWT auth method (`auth/jwt`). The validation flow is the same as cloud IAM federation:

1. Pod presents a projected SA token to Vault (over the network, e.g. via Vault Agent).
2. Vault extracts the `iss` claim.
3. Vault fetches `<issuer>/.well-known/openid-configuration` from the kube-iam-assume bucket.
4. Vault fetches the JWKS and validates the token signature.
5. Vault checks `bound_audiences`, `bound_subject`, and `bound_claims` against the token.
6. Vault returns a Vault token with the configured policies.

Vault never contacts your Kubernetes API server. It only reads from the public bucket.

### Configuration

#### Step 1: Configure the JWT auth method (on Vault)

```bash
vault auth enable jwt

vault write auth/jwt/config \
  oidc_discovery_url="https://my-cluster-oidc.s3.us-west-2.amazonaws.com"
```

Vault fetches `/.well-known/openid-configuration` and extracts the `jwks_uri` automatically. No manual JWKS URL configuration needed.

For HCP Vault, do this via the HCP Vault UI or the Vault CLI pointed at your HCP cluster address.

#### Step 2: Create a role

```bash
vault write auth/jwt/role/my-app \
  role_type="jwt" \
  bound_audiences="https://vault.example.com" \
  bound_subject="system:serviceaccount:production:my-app" \
  user_claim="sub" \
  policies="my-app-policy" \
  ttl="1h"
```

`bound_audiences` must match the `audience` field in the pod's projected token volume. `bound_subject` locks the role to a specific Kubernetes service account.

#### Step 3: Project the token in the pod

```yaml
volumes:
- name: vault-token
  projected:
    sources:
    - serviceAccountToken:
        audience: "https://vault.example.com"   # must match bound_audiences above
        expirationSeconds: 3600
        path: token
```

#### Step 4: Authenticate using Vault Agent

For production use, run Vault Agent as a sidecar or init container. It handles token renewal automatically:

```hcl
# vault-agent-config.hcl
vault {
  address = "https://vault.example.com"
}

auto_auth {
  method "jwt" {
    config = {
      path = "/var/run/secrets/vault/token"
      role = "my-app"
    }
  }
}

sink "file" {
  config = {
    path = "/vault/secrets/.vault-token"
  }
}
```

### Multi-Audience Workloads (Cloud IAM + Vault simultaneously)

A workload that needs both AWS credentials and Vault access requires two projected token volumes — one per audience. A single token cannot serve both: cloud STS rejects tokens whose `aud` is not `sts.amazonaws.com`, and Vault rejects tokens with `sts.amazonaws.com` in `bound_audiences`.

```yaml
volumes:
- name: aws-token
  projected:
    sources:
    - serviceAccountToken:
        audience: "sts.amazonaws.com"
        expirationSeconds: 3600
        path: token

- name: vault-token
  projected:
    sources:
    - serviceAccountToken:
        audience: "https://vault.example.com"
        expirationSeconds: 3600
        path: token

containers:
- name: app
  volumeMounts:
  - name: aws-token
    mountPath: /var/run/secrets/aws
  - name: vault-token
    mountPath: /var/run/secrets/vault
  env:
  - name: AWS_ROLE_ARN
    value: arn:aws:iam::ACCOUNT:role/my-app-role
  - name: AWS_WEB_IDENTITY_TOKEN_FILE
    value: /var/run/secrets/aws/token
```

Both tokens are issued by the same API server and validated against the same kube-iam-assume JWKS. The only difference is the `aud` claim.

### Fine-Grained Access with `bound_claims`

Vault's JWT auth can condition on any claim in the token. Kubernetes 1.21+ tokens include namespace and pod identity under the `kubernetes.io` key, which Vault can use for tighter role binding:

```bash
vault write auth/jwt/role/my-app \
  role_type="jwt" \
  bound_audiences="https://vault.example.com" \
  bound_subject="system:serviceaccount:production:my-app" \
  bound_claims_type="glob" \
  bound_claims='{"kubernetes.io/namespace": "production"}' \
  user_claim="sub" \
  policies="my-app-policy" \
  ttl="1h"
```

`bound_claims` on pod-level fields (`kubernetes.io/pod/name`) is possible but creates ephemeral roles tied to a specific pod name. Only use this for high-privilege workloads where the operational overhead is justified.

### Multi-Cluster Vault Setup

With kube-iam-assume's multi-cluster shared issuer mode, one Vault JWT auth configuration covers every cluster in the group. Configure Vault once with the shared issuer URL:

```bash
vault write auth/jwt/config \
  oidc_discovery_url="https://my-company-oidc.s3.us-west-2.amazonaws.com/prod"
```

All clusters in the `prod` group share this issuer. Their keys are aggregated into the same JWKS. Adding a new cluster to the group requires no Vault-side changes.

### Issuer Migration for Existing Vault Setups

If you previously configured Vault with the old in-cluster issuer (`https://kubernetes.default.svc.cluster.local`) — for example via Vault's Kubernetes auth method configured to accept projected tokens — and are now migrating to kube-iam-assume, update the JWT auth config after deploying kube-iam-assume:

```bash
vault write auth/jwt/config \
  oidc_discovery_url="https://my-cluster-oidc.s3.us-west-2.amazonaws.com"
```

Use dual `--service-account-issuer` values on the API server during the transition so tokens with both issuers remain valid until expiry. See [Issuer Configuration — Safe Migration](#safe-migration).

### CLI Setup Helper (v0.2)

```bash
kube-iam-assume setup vault \
  --issuer-url https://my-cluster-oidc.s3.us-west-2.amazonaws.com \
  --vault-addr https://vault.example.com \
  --audience https://vault.example.com \
  --namespace production \
  --service-account my-app \
  --policy my-app-policy \
  --role my-app
```

This enables the JWT auth method, writes the config, and creates the role in one command.

---

## Publishing Modes

### Object Storage (Recommended)

kube-iam-assume pushes the OIDC discovery document and JWKS to a public cloud storage bucket. The bucket URL becomes the issuer. This requires no inbound network access to the cluster.

| Provider | Service | Public URL Format |
|---|---|---|
| AWS | S3 | `https://BUCKET.s3.REGION.amazonaws.com` |
| GCP | GCS | `https://storage.googleapis.com/BUCKET` |
| Azure | Blob | `https://ACCOUNT.blob.core.windows.net/CONTAINER` |

The bucket must allow public reads (cloud providers fetch the JWKS over HTTPS). Write access must be restricted to kube-iam-assume's credentials only. kube-iam-assume ships hardened bucket policy templates for each provider.

### Built-in HTTPS Endpoint

For environments without object storage, kube-iam-assume can serve the discovery document and JWKS directly via an HTTPS endpoint. This requires:
- A public DNS record pointing to the endpoint
- A valid TLS certificate (kube-iam-assume integrates with cert-manager)
- The endpoint to be reachable from the internet (e.g., behind a load balancer)

This mode is planned for v1.0.

---

## Issuer Configuration

Changing `--service-account-issuer` is the only cluster-level change kube-iam-assume requires.

### What This Flag Does

It sets the `iss` (issuer) claim in all newly issued projected service account tokens. Cloud providers use this claim to locate the OIDC discovery document and fetch the JWKS.

### What It Does NOT Affect

- **Legacy SA tokens** at `/var/run/secrets/kubernetes.io/serviceaccount/token` — not OIDC JWTs, validated via TokenReview.
- **Internal K8s communication** between control plane components — uses client certificates or TokenReview.
- **Pod-to-API-server authentication** — validated via TokenReview regardless of issuer claim.

### What CAN Be Affected

Any external system that validates projected SA tokens by checking the `iss` claim:

- HashiCorp Vault JWT/OIDC auth configured with the old issuer URL
- Istio if configured with a specific issuer expectation
- Custom admission webhooks that validate SA token issuers
- Other federation targets already using the old issuer

### Safe Migration

The API server supports **multiple** `--service-account-issuer` values. The first signs new tokens; additional values are accepted during validation. This enables zero-downtime migration:

```
--service-account-issuer=https://my-cluster-oidc.s3.us-west-2.amazonaws.com
--service-account-issuer=https://kubernetes.default.svc.cluster.local
```

New tokens use the new public URL; existing tokens with the old issuer remain valid until they expire. Once all existing tokens have expired and all dependent systems are reconfigured, the old value can be removed.

---

## Distribution-Specific Guidance

| Distribution | How to Set the Flag |
|---|---|
| kubeadm | `ClusterConfiguration.apiServer.extraArgs` in kubeadm config |
| k3s | `--kube-apiserver-arg service-account-issuer=<url>` |
| RKE2 | `kube-apiserver-arg` in `/etc/rancher/rke2/config.yaml` |
| Talos | `cluster.apiServer.extraArgs` in machine config |
| minikube | `--extra-config=apiserver.service-account-issuer=<url>` |
| kind | `kubeadmConfigPatches` in kind config |

---

## Bucket Naming

### Why Bucket Names Matter for Security

The S3 bucket name is embedded directly in the OIDC issuer URL. It appears in the API server flag, every projected service account JWT, and the cloud provider's OIDC registration. An attacker who can enumerate and gain write access to the bucket can replace the JWKS with their own keys and forge tokens.

Obfuscating the bucket name prevents untargeted discovery. This is defence-in-depth — proper write-access restrictions are the real defence.

### Naming Strategies

| Strategy | How it works | Entropy | Recommended for |
|---|---|---|---|
| Manual (default) | User provides name directly | None | Dev/testing |
| Prefix + UUID | `<prefix>-<truncated-uuid-v4>` | 80 bits | Production |

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc \
  --region=us-west-2 \
  --cluster=prod-us-west-2
```

Output:

```
Bucket name:  oidc-a3f8c1b9e4d27f6b2a91
Issuer URL:   https://oidc-a3f8c1b9e4d27f6b2a91.s3.us-west-2.amazonaws.com
```

### Output Formats

`--output=configmap` — writes a Kubernetes ConfigMap:

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc --region=us-west-2 --cluster=prod-us-west-2 \
  --output=configmap | kubectl apply -f -
```

`--output=json` — for scripting and CI/CD.

`--output=helm` — for direct use with `helm install -f`.

### Automatic Tags

kube-iam-assume tags the bucket automatically so it remains identifiable:

| Tag Key | Example Value |
|---|---|
| `kube-iam-assume/managed-by` | `kube-iam-assume` |
| `kube-iam-assume/cluster` | `prod-us-west-2` |
| `kube-iam-assume/prefix` | `oidc` |
| `kube-iam-assume/created-at` | `2026-02-09T12:00:00Z` |
| `kube-iam-assume/issuer-url` | `https://oidc-a3f8...amazonaws.com` |

### Reverse Lookup

```bash
# From the ConfigMap in the cluster
kube-iam-assume get-bucket-info --from=configmap

# From bucket tags in your cloud account
kube-iam-assume get-bucket-info --from=s3-tags --region=us-west-2 --cluster=prod-us-west-2
```

### Caveats

- The issuer URL is not truly secret. It appears in every JWT the cluster issues. Obfuscation prevents untargeted scanning, not insider access.
- The bucket must still allow public reads. Obfuscating the name does not change the access policy.
- The primary defence is write-access restriction, not name obfuscation.

---

## Security Model

### What Is Publicly Exposed

Only the JWKS (public signing keys) and the OIDC discovery document. This is identical to what EKS, GKE, and AKS publish for every managed cluster. Public keys allow token **verification**, not **forgery**. The private signing key never leaves the API server.

### Token Properties

Projected service account tokens are:

- **Audience-bound:** include a specific `aud` claim (e.g., `sts.amazonaws.com`) and are rejected if the audience does not match
- **Time-bound:** expire (default 1 hour, configurable)
- **Identity-bound:** `sub` = `system:serviceaccount:<namespace>:<name>`, constrained by the cloud IAM trust policy
- **Pod-bound:** invalidated when the pod is deleted

### Identity Granularity

The `sub` claim is SA-scoped, not pod-scoped. Two pods sharing a service account produce tokens with identical `sub` values. Authorization granularity is at the service account level. Use one SA per distinct workload identity.

#### Pod-Level Claims

Kubernetes 1.21+ tokens carry pod-level claims under the `kubernetes.io` key:

```json
{
  "sub": "system:serviceaccount:production:my-app",
  "kubernetes.io": {
    "namespace": "production",
    "pod":            { "name": "my-app-7d9f8b-xkz2p", "uid": "abc-123" },
    "serviceaccount": { "name": "my-app",               "uid": "xyz-789" },
    "node":           { "name": "node-1",               "uid": "node-uid" }
  }
}
```

| Cloud | Pod-level claim support |
|---|---|
| AWS | No — trust policy conditions only support `sub` and `aud` |
| GCP | Yes — attribute mapping (CEL) can expose pod claims as custom attributes |
| Azure | No — federated credential conditions are limited to `sub` |

On GCP, kube-iam-assume configures attribute mappings for `pod_name`, `pod_uid`, `namespace`, and `service_account_name` automatically.

#### For Per-Pod Identity

If you need per-pod (or per-process) identity, use [SPIFFE/SPIRE](https://spiffe.io). kube-iam-assume operates at the service account level.

### Blast Radius

A compromised pod can obtain temporary credentials only for the specific IAM role bound to its service account. It cannot escalate to other roles or other namespaces. The cloud-side trust policy is the enforcement point.

### Object Storage Security

The publishing bucket must be publicly readable but must restrict write access to the kube-iam-assume controller only. If an attacker gains write access, they can replace the JWKS and forge tokens. kube-iam-assume ships hardened bucket policy templates and validates bucket permissions on startup.

**CIDR restrictions on buckets are not recommended.** Cloud provider STS services fetch the JWKS from a wide, unpublished range of IP addresses that change over time. CIDR restrictions will cause STS to fail JWKS retrieval and break federation silently.

### Signing Key Compromise

If the API server's signing key is compromised, an attacker can forge tokens for any service account. This risk is identical to managed Kubernetes. Mitigate by restricting control plane access, monitoring cloud audit logs for anomalous `AssumeRoleWithWebIdentity` calls, and using kube-iam-assume's rotation support to roll to new keys.

---

## Roadmap

### v0.1 — The Bridge

- OIDC bridge controller with S3 publishing
- Automatic JWKS rotation with dual-publish overlap
- `generate-bucket-name` CLI with tagging and multiple output formats
- `get-bucket-info` reverse-lookup CLI
- `setup aws` CLI for OIDC IdP registration
- `status` for sync health and diagnostics
- Helm chart

### v0.2 — Multi-Cloud

- GCS and Azure Blob publishing modes
- `setup gcp` and `setup azure` CLI commands
- Terraform modules for cloud-side OIDC IdP registration
- Prometheus metrics

### v0.3 — Policy Layer

- `CloudIdentityBinding` CRD for declarative SA-to-cloud-identity mapping
- Mutating webhook for automatic projected volume and env var injection
- Validating webhook for policy enforcement
- `kubectl get cloudidentitybindings -A` for cross-namespace audit

### v1.0 — Production Ready

- Built-in HTTPS endpoint mode
- Comprehensive documentation
- Security model and threat analysis
- CNCF Landscape submission

---

## FAQ

**What happens if kube-iam-assume goes down?**

Cloud providers cache the JWKS. If the controller stops publishing, existing cached keys remain valid until the cache expires (varies by provider, typically hours). Existing pods continue to work. kube-iam-assume publishes to durable object storage, so even if the controller pod restarts, the published JWKS remains available.

**Does kube-iam-assume replace SPIRE?**

No. See [README.md — kube-iam-assume vs SPFFE/SPIRE](README.md#kube-iam-assume-vs-spiffespire) for a full comparison.

**Can I use kube-iam-assume with EKS/GKE/AKS?**

You don't need to. These managed services already publish OIDC endpoints.

**Does changing the service account issuer break anything?**

It does not affect legacy SA tokens, internal K8s communication, or pod-to-API-server auth. It can affect systems that validate the `iss` claim of projected tokens. See [Issuer Configuration](#issuer-configuration) for the full analysis and safe migration procedure.

**What Kubernetes versions are supported?**

Kubernetes 1.22 and above. Projected service account tokens (stable in 1.20) and service account issuer discovery (GA in 1.21) are both required.

**What about air-gapped environments?**

If your cluster has no internet egress at all, the cloud provider STS endpoint is also unreachable, so OIDC federation is not applicable. If you have egress to cloud APIs but no inbound access, the object storage publishing mode works perfectly — kube-iam-assume pushes outbound to the bucket; cloud providers read from the bucket.

**How is this different from just uploading JWKS to S3 manually?**

kube-iam-assume automates what you would otherwise do with a script and adds: automatic key rotation detection and dual-publish handling, health checking, Prometheus metrics, CLI helpers for cloud provider registration, and obfuscated bucket name generation. If you rotate your SA signing keys and forget to update the bucket, federation breaks silently. kube-iam-assume prevents that.

**How do I differentiate two pods that share the same service account?**

You can't — by design. The `sub` claim is bound to the SA, not the pod. Use one SA per workload identity. On GCP, kube-iam-assume maps `kubernetes.io` pod claims as custom attributes, enabling conditions targeting specific pods. For workload-level identity, use SPIFFE/SPIRE.
