# kube-iam-assume

**Secretless cloud access for any Kubernetes cluster.**

kube-iam-assume brings EKS-style IAM Roles for Service Accounts (IRSA) to every self-hosted Kubernetes cluster — kubeadm, k3s, RKE, Talos, bare-metal — in under 5 minutes. No sidecars. No service mesh. No agents. One controller, two JSON files, zero secrets.

---

## Table of Contents

- [The Problem](#the-problem)
- [How It Works](#how-it-works)
- [Quick Start](#quick-start)
- [Issuer Configuration](#issuer-configuration)
- [Key Rotation](#key-rotation)
- [Multi-Cluster Shared Issuer](#multi-cluster-shared-issuer)
- [Publishing Modes](#publishing-modes)
- [Bucket Naming](#bucket-naming)
- [Multi-Cloud Support](#multi-cloud-support)
- [Architecture](#architecture)
- [Comparison with Alternatives](#comparison-with-alternatives)
- [Security Model](#security-model)
- [Roadmap](#roadmap)
- [FAQ](#faq)
- [Contributing](#contributing)
- [Acknowledgements](#acknowledgements)
- [License](#license)

---

## The Problem

Every major managed Kubernetes service has solved workload identity federation:

| Managed Service | Solution | How It Works |
|---|---|---|
| EKS | IRSA | Public OIDC endpoint per cluster |
| GKE | Workload Identity | Public OIDC endpoint per cluster |
| AKS | Workload Identity Federation | Public OIDC endpoint per cluster |

The mechanism is identical across all three: the managed service publishes the cluster's OIDC discovery endpoint publicly so cloud IAM can validate Kubernetes service account tokens and issue temporary credentials.

**Self-hosted Kubernetes has none of this.**

The API server's OIDC endpoints (`/.well-known/openid-configuration` and `/openid/v1/jwks`) are private. Cloud providers cannot reach them. Federation is impossible. Teams fall back to embedding long-lived cloud credentials as Kubernetes Secrets — the number one source of cloud credential leaks.

The irony: Kubernetes already issues perfectly valid OIDC-compliant JWT tokens. Cloud providers already accept them via `AssumeRoleWithWebIdentity` (AWS), Workload Identity Federation (GCP), and Federated Credentials (Azure). The only missing piece is making the public keys reachable.

That is all kube-iam-assume does.

### Why Now

Three converging trends make this the right time:

**The GPU cloud explosion.** The AI infrastructure wave is creating thousands of new self-hosted Kubernetes clusters on bare-metal GPU hardware. Every one of them needs to connect to cloud services (S3 for model storage at minimum) and most are using static credentials today.

**Kubernetes projected service account tokens are mature.** The `ServiceAccountTokenVolumeProjection` feature has been stable since K8s 1.20. The `ServiceAccountIssuerDiscovery` feature has been GA since 1.21. The infrastructure is ready. The bridge is the only missing piece.

**Cloud providers all support OIDC federation.** AWS added `AssumeRoleWithWebIdentity` in 2014. GCP launched Workload Identity Federation in 2021. Azure launched Workload Identity Federation in 2022. The receiving end is built. Nobody built the sending end for self-hosted K8s.

---

## How It Works

kube-iam-assume is a single Kubernetes controller that:

1. Fetches your cluster's OIDC discovery document and JWKS from the API server (in-cluster, authenticated)
2. Publishes them to a publicly reachable location (S3 bucket, GCS bucket, Azure Blob, or a built-in HTTPS endpoint)
3. Monitors for signing key rotation and handles it automatically with zero-downtime dual-key publishing

Once running, your self-hosted cluster is indistinguishable from EKS/GKE/AKS from the cloud provider's perspective.

```
Without kube-iam-assume:
  Developer -> creates access key -> stores in K8s Secret -> rotates manually -> hopes nobody leaks it

With kube-iam-assume:
  Pod starts -> K8s issues SA token -> SDK calls STS -> gets temp creds (auto-expires) -> done
```

### The Token Exchange Flow

When a workload presents a K8s service account token to a cloud provider (e.g., AWS STS), the cloud provider performs this validation:

1. Extract the `iss` (issuer) claim from the JWT.
2. Fetch `<issuer>/.well-known/openid-configuration`.
3. Extract `jwks_uri` from the discovery document.
4. Fetch the JWKS (public keys) from that URI.
5. Validate the JWT signature against the public keys.
6. Check `sub` and `aud` claims match the cloud IAM trust policy conditions.
7. Return temporary cloud credentials.

kube-iam-assume makes step 2-4 possible for self-hosted clusters by publishing the discovery document and JWKS at a publicly reachable URL.

---

## Quick Start

### Prerequisites

- Kubernetes 1.22+ (projected service account token support)
- Access to modify API server flags (one-time change)
- An S3 bucket (or GCS bucket, or Azure Blob container) with public read access

### Step 1: Configure the API Server Issuer

Add the public URL as the primary service account issuer, and keep the old issuer as a secondary value so existing tokens remain valid. See [Issuer Configuration](#issuer-configuration) for details and safe migration guidance.

```
--service-account-issuer=https://my-cluster-oidc.s3.us-west-2.amazonaws.com   # new primary (used for signing)
--service-account-issuer=https://kubernetes.default.svc.cluster.local          # old (still accepted for validation)
```

The first value signs new tokens. The second value ensures existing tokens issued with the old issuer are still accepted until they expire. Once all old tokens have expired, the second line can be removed.

### Step 1.5: Generate an Obfuscated Bucket Name (Recommended for Production)

```bash
kube-iam-assume generate-bucket-name --prefix=oidc --region=us-west-2
```

This generates a non-guessable bucket name like `oidc-a3f8c1b9e4d27f6b2a91`. Use the generated name in all subsequent steps instead of a manually chosen name. See [Bucket Naming](#bucket-naming) for details.

### Step 2: Install kube-iam-assume

```bash
helm install kube-iam-assume kube-iam-assume/kube-iam-assume \
  --set publishMode=s3 \
  --set s3.bucket=my-cluster-oidc \
  --set s3.region=us-west-2
```

### Step 3: Register with Your Cloud Provider

```bash
kube-iam-assume setup aws \
  --issuer-url https://my-cluster-oidc.s3.us-west-2.amazonaws.com
```

This wraps the AWS CLI calls to create the IAM OIDC Identity Provider — a one-time operation.

### Step 4: Create an IAM Role Trust Policy

```json
{
  "Effect": "Allow",
  "Principal": {
    "Federated": "arn:aws:iam::ACCOUNT:oidc-provider/my-cluster-oidc.s3.us-west-2.amazonaws.com"
  },
  "Action": "sts:AssumeRoleWithWebIdentity",
  "Condition": {
    "StringEquals": {
      "my-cluster-oidc.s3.us-west-2.amazonaws.com:sub": "system:serviceaccount:production:my-app"
    }
  }
}
```

### Step 5: Deploy Your Workload

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  serviceAccountName: my-app
  containers:
  - name: app
    image: my-app:latest
    env:
    - name: AWS_ROLE_ARN
      value: arn:aws:iam::ACCOUNT:role/my-app-role
    - name: AWS_WEB_IDENTITY_TOKEN_FILE
      value: /var/run/secrets/kube-iam-assume/token
    volumeMounts:
    - name: aws-token
      mountPath: /var/run/secrets/kube-iam-assume
  volumes:
  - name: aws-token
    projected:
      sources:
      - serviceAccountToken:
          audience: sts.amazonaws.com
          expirationSeconds: 3600
          path: token
```

The AWS SDK detects the environment variables, reads the projected token, calls `sts:AssumeRoleWithWebIdentity`, and your application has temporary credentials. No access keys anywhere.

---

## Issuer Configuration

Changing `--service-account-issuer` is the only cluster-level change kube-iam-assume requires. This section explains exactly what it affects and how to migrate safely.

### What This Flag Does

The `--service-account-issuer` flag sets the `iss` (issuer) claim in all newly issued projected service account tokens. Cloud providers use this claim to locate the OIDC discovery document: they fetch `<iss>/.well-known/openid-configuration` to find the JWKS and validate the token signature.

For federation to work, the issuer URL in the token must match the URL where kube-iam-assume publishes the JWKS. If they don't match, the cloud provider rejects the token.

### What This Does NOT Affect

- **Legacy SA tokens** mounted at `/var/run/secrets/kubernetes.io/serviceaccount/token` are unaffected. These are not OIDC JWTs and are validated via the TokenReview API, not OIDC discovery.
- **Internal K8s communication** between the API server, kubelet, controller-manager, and scheduler is unaffected. These components authenticate via client certificates or TokenReview, not OIDC.
- **Pod-to-API-server authentication** is unaffected. The API server validates its own tokens via TokenReview regardless of the issuer claim.

### What CAN Be Affected

If you have existing systems that validate projected SA tokens by checking the `iss` claim, those systems will reject tokens with the new issuer until reconfigured. Common examples:

- HashiCorp Vault JWT/OIDC auth configured with the old issuer URL
- Istio if configured with a specific issuer expectation
- Custom admission webhooks that validate SA token issuers
- Other federation targets already using the old issuer

### Safe Migration

The API server supports **multiple** `--service-account-issuer` values. The first value is the primary issuer (used for signing new tokens). Additional values are accepted during token validation. This enables zero-downtime migration:

```
--service-account-issuer=https://my-cluster-oidc.s3.us-west-2.amazonaws.com   # new primary
--service-account-issuer=https://kubernetes.default.svc.cluster.local          # old, still accepted
```

With this configuration:
- New tokens are issued with the new public URL as their issuer
- Existing tokens with the old issuer remain valid until they expire
- Systems validating the old issuer continue to work
- Cloud providers can validate new tokens via the public JWKS

Once all existing tokens have expired and all dependent systems are reconfigured, the old issuer value can be removed.

### Distribution-Specific Guidance

| Distribution | How to Set the Flag |
|---|---|
| kubeadm | `ClusterConfiguration.apiServer.extraArgs` in kubeadm config |
| k3s | `--kube-apiserver-arg service-account-issuer=<url>` |
| RKE2 | `kube-apiserver-arg` in `/etc/rancher/rke2/config.yaml` |
| Talos | `cluster.apiServer.extraArgs` in machine config |
| minikube | `--extra-config=apiserver.service-account-issuer=<url>` |
| kind | `kubeadmConfigPatches` in kind config |

---

## Key Rotation

kube-iam-assume handles signing key rotation automatically.

The controller polls the API server's `/openid/v1/jwks` endpoint every 60 seconds (configurable). When a key change is detected:

1. The new key set is merged with the previous key set.
2. The merged JWKS (containing both old and new keys) is published.
3. After the overlap period (default: 24 hours), the old keys are removed.

During the overlap window, tokens signed by either key set are valid. This is the same strategy EKS uses for its managed OIDC endpoints.

| State | Published JWKS | Valid Tokens | Duration |
|---|---|---|---|
| Steady | [Key A] | Signed by Key A | Indefinite |
| Rotation detected | [Key A, Key B] | Signed by A or B | Overlap period (default 24h) |
| Overlap expired | [Key B] | Signed by Key B | Indefinite |

The controller emits Kubernetes Events on every rotation and exposes Prometheus metrics for monitoring.

---

## Multi-Cluster Shared Issuer

> **Default behavior is unchanged.** Each cluster gets its own unique issuer URL. This section describes an opt-in feature for environments that run multiple clusters and want workloads to move across them without YAML changes.

### The Problem It Solves

In a standard single-cluster setup, the OIDC issuer URL is tied to that cluster's bucket:

```
https://my-cluster-oidc.s3.us-west-2.amazonaws.com
```

Cloud IAM trust policies reference this URL. If a workload moves to a different cluster (blue/green, region failover, horizontal scaling), the new cluster has a different issuer URL, and the same IAM trust policy no longer matches. You must update every trust policy — or maintain duplicate policies per cluster.

The multi-cluster shared issuer feature solves this: all clusters in the same group share one issuer URL, one JWKS endpoint, and therefore one set of IAM trust policies.

### Why This Works

Every Kubernetes cluster generates its own RSA/ECDSA signing key pair. Each key has a unique `kid` (Key ID, derived from the SHA-256 hash of the public key). No two clusters share the same `kid`.

When a pod on **any cluster in the group** presents a token to AWS STS:

1. Token `iss` = shared group issuer URL → matches the IAM trust policy ✓
2. Token is signed with this cluster's private key; `kid` identifies which key
3. AWS fetches the aggregated JWKS → finds the right public key by `kid` → validates signature ✓
4. Token `sub` = `system:serviceaccount:<namespace>:<name>` → identical for the same service account across all clusters ✓

Same YAML, same IAM role annotation, works on every cluster in the group.

### Modes of Operation

#### Mode 1: Single-Cluster (Default)

`clusterGroup` is not set. Each cluster publishes independently with its own issuer URL. This is the default and requires no configuration changes.

```yaml
# config.yaml — single-cluster mode (default, no changes needed)
controller:
  syncPeriod: 60s
  rotationOverlap: 24h

publisher:
  type: s3
  s3:
    bucket: my-cluster-oidc
    region: us-west-2
# Issuer URL: https://my-cluster-oidc.s3.us-west-2.amazonaws.com
```

#### Mode 2: Multi-Cluster Shared Issuer (Opt-In)

Set `clusterGroup` and `clusterID` on each cluster. All clusters with the same `clusterGroup` share one issuer URL.

```yaml
# Cluster A (prod-us-west-2) — config.yaml
controller:
  clusterGroup: prod        # groups clusters sharing one issuer URL
  clusterID: prod-us-west-2 # unique name for this cluster within the group

publisher:
  type: s3
  s3:
    bucket: my-company-oidc # shared bucket for the whole group
    region: us-west-2
    # prefix is set automatically from clusterGroup — do not set manually
# Issuer URL: https://my-company-oidc.s3.us-west-2.amazonaws.com/prod
```

```yaml
# Cluster B (prod-eu-west-1) — config.yaml
controller:
  clusterGroup: prod          # same group → same issuer URL
  clusterID: prod-eu-west-1   # different ID → no write conflict

publisher:
  type: s3
  s3:
    bucket: my-company-oidc   # same bucket
    region: us-west-2
# Issuer URL: https://my-company-oidc.s3.us-west-2.amazonaws.com/prod  (identical)
```

```yaml
# Staging cluster — separate group, isolated from prod
controller:
  clusterGroup: staging
  clusterID: staging-us-east-1

publisher:
  type: s3
  s3:
    bucket: my-company-oidc   # same bucket is fine — isolated by prefix
    region: us-west-2
# Issuer URL: https://my-company-oidc.s3.us-west-2.amazonaws.com/staging
```

With this setup, a single IAM trust policy covers all prod clusters:

```json
{
  "Condition": {
    "StringEquals": {
      "my-company-oidc.s3.us-west-2.amazonaws.com/prod:sub":
        "system:serviceaccount:production:my-app"
    }
  }
}
```

No YAML changes needed when adding clusters or moving workloads between `prod` clusters.

### Storage Layout

```
s3://my-company-oidc/
  prod/                                      ← clusterGroup = "prod"
    .well-known/openid-configuration         ← issuer = .../prod (all clusters identical)
    openid/v1/jwks                           ← aggregated: union of all prod cluster keys
    clusters/
      prod-us-west-2/openid/v1/jwks          ← written by cluster A only
      prod-eu-west-1/openid/v1/jwks          ← written by cluster B only
      prod-ap-northeast-1/openid/v1/jwks     ← written by cluster C only
  staging/                                   ← clusterGroup = "staging", fully isolated
    .well-known/openid-configuration
    openid/v1/jwks
    clusters/
      staging-us-east-1/openid/v1/jwks
```

Each cluster writes **only to its own sub-path** (`clusters/<clusterID>/openid/v1/jwks`). The aggregated root JWKS (`openid/v1/jwks`) is written by the **elected leader** across all clusters in the group, on a configurable interval (default: 5 minutes). Cloud providers read the aggregated JWKS, which contains the union of all clusters' public keys.

The same layout applies to GCS, Azure Blob, and OCI Object Storage with their respective URL formats.

### Helm Configuration

```yaml
# values.yaml

config:
  controller:
    clusterGroup: "prod"           # set to enable multi-cluster mode; empty = single-cluster
    clusterID: "prod-us-west-2"    # unique ID within the group (required if clusterGroup is set)
    aggregationInterval: "5m"      # how often the leader merges cluster JWKS (default: 5m)
    clusterTTL: "48h"              # remove clusters from aggregation if idle for this long (default: 48h)
```

`clusterGroup` must match `^[a-z0-9][a-z0-9-]*[a-z0-9]$` (lowercase DNS-label format). The same validation applies to `clusterID`.

### Cluster TTL and Decommissioning

When a cluster is permanently removed from the group, its per-cluster JWKS sub-path becomes stale. The `clusterTTL` setting (default: 48 hours) controls how long the leader waits before excluding a cluster from aggregation if it hasn't published a JWKS update. After the TTL passes, the cluster's keys are dropped from the aggregated JWKS automatically — no manual cleanup needed.

To decommission a cluster immediately: delete its `clusters/<clusterID>/` sub-path in the bucket. The next aggregation cycle will omit it.

### Registration with AWS (Multi-Cluster Example)

The OIDC Identity Provider is registered once per group, not once per cluster:

```bash
# Register once for the entire group
kube-iam-assume setup aws \
  --issuer-url https://my-company-oidc.s3.us-west-2.amazonaws.com/prod

# This IAM trust policy works for every cluster in the prod group
```

Adding a new cluster to the group requires no cloud-side changes. Install kube-iam-assume on the new cluster with the same `clusterGroup` value, and it begins contributing its keys to the aggregated JWKS within one aggregation interval.

### Configuration Reference

| Field | Default | Description |
|---|---|---|
| `controller.clusterGroup` | `""` | Group name; empty disables multi-cluster mode |
| `controller.clusterID` | `""` | Unique cluster ID within the group; required when `clusterGroup` is set |
| `controller.aggregationInterval` | `"5m"` | How often the elected leader aggregates all cluster JWKS |
| `controller.clusterTTL` | `"48h"` | Exclude clusters from aggregation after this idle duration |

---

## Publishing Modes

### Object Storage (Recommended)

kube-iam-assume pushes the OIDC discovery document and JWKS to a public cloud storage bucket. The bucket URL becomes the issuer. This requires no inbound network access to the cluster.

Supported targets:
- Amazon S3
- Google Cloud Storage
- Azure Blob Storage

The bucket must allow public read access (cloud providers fetch the JWKS over HTTPS). Write access must be restricted to kube-iam-assume's credentials only. kube-iam-assume ships hardened bucket policy templates for each provider.

### Built-in HTTPS Endpoint

For environments without object storage, kube-iam-assume can serve the OIDC discovery document and JWKS directly via an HTTPS endpoint. This requires:
- A public DNS record pointing to the endpoint
- A valid TLS certificate (kube-iam-assume integrates with cert-manager)
- The endpoint to be reachable from the internet (e.g., behind a load balancer)

---

## Bucket Naming

### Why Bucket Names Matter for Security

When using the object storage publishing mode, the S3 bucket name is embedded directly in the OIDC issuer URL (`https://<bucket>.s3.<region>.amazonaws.com`). This URL appears in:

- The API server `--service-account-issuer` flag
- The `iss` claim of every projected service account JWT
- The cloud provider's OIDC Identity Provider registration

If an attacker can guess your bucket name, they can attempt a targeted attack chain:

1. **Enumeration** — scan for predictable bucket names (e.g., `<company>-oidc`, `<cluster>-jwks`)
2. **Discovery** — confirm the bucket exists and contains OIDC metadata
3. **Write exploit** — attempt to gain write access via misconfigured bucket policies, compromised credentials, or social engineering
4. **JWKS replacement** — upload a JWKS containing the attacker's own public key
5. **Token forgery** — forge service account tokens that cloud providers accept as valid

Obfuscating the bucket name prevents untargeted discovery at step 1. This is defense-in-depth — not a primary security control. Proper bucket write-access restrictions are the real defense (see [Object Storage Security](#object-storage-security)).

### Naming Strategies

kube-iam-assume supports two bucket naming strategies. The bucket name must be decided before installation because it becomes the issuer URL embedded in the API server configuration.

| Strategy | How it works | Entropy | Recommended for |
|---|---|---|---|
| Manual (default) | User provides name directly | None | Dev/testing |
| Prefix + UUID (recommended) | `<prefix>-<truncated-uuid-v4>` | 80 bits | Production |

**Manual** is the default — you pick a name like `my-cluster-oidc`. Simple, but guessable. Fine for development and testing.

**Prefix + UUID** combines a human-readable prefix (e.g., `oidc`, your team name) with a truncated UUID v4 for randomness. The prefix keeps the name recognizable in your AWS console; the UUID suffix makes it unguessable by scanners. This is the recommended strategy for production.

The bucket name only contains public keys, so the goal is not secrecy — it is making untargeted scanning impractical. A random suffix achieves that without adding operational complexity.

### Bucket Tags and Labels

When the bucket name is obfuscated, you lose the ability to look at a bucket and know what it belongs to. kube-iam-assume applies metadata tags (S3 tags / GCS labels) to the bucket so it remains identifiable and manageable.

Tags applied automatically:

| Tag Key | Example Value | Purpose |
|---|---|---|
| `kube-iam-assume/managed-by` | `kube-iam-assume` | Identify buckets managed by this tool |
| `kube-iam-assume/cluster` | `prod-us-west-2` | Which cluster this bucket serves |
| `kube-iam-assume/prefix` | `oidc` | The human-readable prefix used during generation |
| `kube-iam-assume/created-at` | `2026-02-09T12:00:00Z` | When the bucket was created |
| `kube-iam-assume/issuer-url` | `https://oidc-a3f8...s3.us-west-2.amazonaws.com` | The full issuer URL for quick reference |

You can also add custom tags via `--tags`:

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc \
  --region=us-west-2 \
  --cluster=prod-us-west-2 \
  --tags="team=platform,env=production"
```

These tags let you filter and audit buckets later — for example, `aws s3api list-buckets` combined with `get-bucket-tagging` to find all kube-iam-assume-managed buckets across your account.

### CLI Command

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc \
  --region=us-west-2 \
  --cluster=prod-us-west-2
```

Default output:

```
Bucket name:  oidc-a3f8c1b9e4d27f6b2a91
Issuer URL:   https://oidc-a3f8c1b9e4d27f6b2a91.s3.us-west-2.amazonaws.com
Region:       us-west-2

Use this issuer URL in your API server configuration:
  --service-account-issuer=https://oidc-a3f8c1b9e4d27f6b2a91.s3.us-west-2.amazonaws.com

And in your Helm install:
  --set s3.bucket=oidc-a3f8c1b9e4d27f6b2a91
  --set s3.region=us-west-2
```

### Output Formats

The generated bucket name, issuer URL, and tags need to be persisted — you will reference them in multiple places (API server flags, Helm values, cloud provider registration). The CLI supports multiple output formats to fit different workflows.

**ConfigMap** (`--output=configmap`) — writes a Kubernetes ConfigMap that the controller and other tools can reference:

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc --region=us-west-2 --cluster=prod-us-west-2 \
  --output=configmap | kubectl apply -f -
```

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kube-iam-assume-bucket
  namespace: kube-iam-assume
  labels:
    app.kubernetes.io/managed-by: kube-iam-assume
data:
  bucket: oidc-a3f8c1b9e4d27f6b2a91
  region: us-west-2
  issuerUrl: https://oidc-a3f8c1b9e4d27f6b2a91.s3.us-west-2.amazonaws.com
  cluster: prod-us-west-2
  prefix: oidc
  createdAt: "2026-02-09T12:00:00Z"
```

**JSON** (`--output=json`) — for scripting and CI/CD pipelines:

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc --region=us-west-2 --cluster=prod-us-west-2 \
  --output=json > bucket-config.json
```

**Helm values** (`--output=helm`) — outputs a values file snippet you can pass directly to `helm install -f`:

```bash
kube-iam-assume generate-bucket-name \
  --prefix=oidc --region=us-west-2 \
  --output=helm > bucket-values.yaml

helm install kube-iam-assume kube-iam-assume/kube-iam-assume -f bucket-values.yaml
```

### Reverse Lookup

To recover the issuer URL for an existing obfuscated bucket, use `kube-iam-assume get-bucket-info`:

```bash
# From the ConfigMap in the cluster
kube-iam-assume get-bucket-info --from=configmap

# From bucket tags in your cloud account
kube-iam-assume get-bucket-info --from=s3-tags --region=us-west-2 --cluster=prod-us-west-2
```

This scans for buckets tagged with `kube-iam-assume/managed-by` and matching the specified cluster, then outputs the issuer URL and bucket metadata.

This is a pre-install utility. The bucket name must be decided before running `helm install` because the API server `--service-account-issuer` flag must match the bucket URL.

### Important Caveats

- **The issuer URL is not truly secret.** It appears in the `iss` claim of every JWT your cluster issues. Any workload running on the cluster can read it. Any cloud IAM admin who registered the OIDC provider can see it. Bucket name obfuscation prevents *untargeted scanning*, not *targeted access by insiders*.
- **This is defense-in-depth only.** The primary defense against JWKS replacement is restricting write access to the bucket. Bucket name obfuscation is an additional layer, not a substitute.
- **The bucket must still allow public reads.** Cloud provider STS services must be able to fetch the JWKS over HTTPS. Obfuscating the name does not change the bucket's access policy.

---

## Multi-Cloud Support

kube-iam-assume publishes standard OIDC discovery metadata. Any system that supports OIDC-based identity federation can consume it.

| Cloud Provider | Federation Mechanism | CLI Helper |
|---|---|---|
| AWS | IAM OIDC Identity Provider + `AssumeRoleWithWebIdentity` | `kube-iam-assume setup aws` |
| GCP | Workload Identity Federation Pool + Provider | `kube-iam-assume setup gcp` |
| Azure | Federated Identity Credentials | `kube-iam-assume setup azure` |

A single kube-iam-assume installation supports federation with all three cloud providers simultaneously. The OIDC metadata is cloud-agnostic; only the cloud-side registration differs.

---

## Architecture

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

kube-iam-assume consists of a single Deployment with one container. No DaemonSets, no sidecars, no node-level agents. It requires cluster-internal read access to the OIDC endpoints and write access to the publishing target.

### Components (Current and Planned)

| Component | Status | Description |
|---|---|---|
| OIDC Bridge Controller | v0.1 | Publishes and syncs JWKS to public endpoint, handles rotation |
| CLI (`kube-iam-assume`) | v0.1 | One-time cloud provider OIDC IdP registration and diagnostics |
| Helm Chart | v0.1 | Single-command installation |
| Prometheus Metrics | v0.2 | Sync count, rotation count, publish latency, errors |
| Terraform Modules | v0.2 | Cloud-side OIDC IdP registration for AWS, GCP, Azure |
| `CloudIdentityBinding` CRD | v0.3 | Declarative K8s SA to cloud identity mapping |
| Mutating Webhook | v0.3 | Auto-injects projected volume and cloud env vars into pods |

---

## Comparison with Alternatives

| | kube-iam-assume | SPIFFE/SPIRE | Manual S3 Upload | Azure Workload Identity |
|---|---|---|---|---|
| Auto JWKS publish | Yes | Yes | No (manual) | No |
| Auto key rotation | Yes (dual-publish) | Yes | No (manual) | No |
| Multi-cloud | AWS, GCP, Azure | Any OIDC consumer | Per-cloud setup | Azure only |
| Operational overhead | Single Deployment | Server + agents on every node + registration entries | Scripts + cron | Webhook only |
| Cluster changes | Issuer flag only | Issuer flag + SPIRE infrastructure | Issuer flag only | Issuer flag + webhook |
| Workload changes | Projected volume + env vars | SPIRE Workload API | Projected volume + env vars | Annotations |
| Time to production | ~5 minutes | Hours to days | ~30 minutes | ~15 minutes (Azure only) |

**When to use kube-iam-assume:** You run self-hosted K8s and need your workloads to access cloud services without static credentials. You want the simplest possible solution.

**When to use SPIRE instead:** You need a full workload identity mesh across heterogeneous environments (VMs, containers, multi-cloud), mTLS between services, or identity attestation beyond service account names.

---

## Security Model

### What Is Publicly Exposed

Only the JWKS (public signing keys) and the OIDC discovery document are made public. This is identical to what EKS, GKE, and AKS publish for every managed cluster. Public keys allow token **verification**, not token **forging**. The private signing key never leaves the API server.

### Token Properties

Projected service account tokens used with kube-iam-assume are:

- **Audience-bound:** tokens include a specific `aud` claim (e.g., `sts.amazonaws.com`) and are rejected by STS if the audience doesn't match
- **Time-bound:** tokens expire (default 1 hour, configurable)
- **Identity-bound:** the `sub` claim contains `system:serviceaccount:<namespace>:<name>`, and the cloud IAM trust policy restricts which subjects can assume each role
- **Pod-bound:** tokens are bound to the specific pod and are invalidated if the pod is deleted

### Blast Radius

A compromised pod can only obtain temporary credentials for the specific IAM role bound to its service account. It cannot escalate to other roles or other namespaces. The cloud-side trust policy is the enforcement point.

### Object Storage Security

The publishing bucket must be publicly readable (cloud providers fetch JWKS over HTTPS) but must restrict write access to only the kube-iam-assume controller. If an attacker gains write access to the bucket, they could replace the JWKS with their own keys and forge tokens. kube-iam-assume ships hardened bucket policy templates and validates bucket permissions on startup.

**Obfuscated bucket names:** kube-iam-assume provides a `generate-bucket-name` CLI command that produces non-guessable bucket names using a prefix + UUID strategy. This prevents untargeted discovery via bucket name scanning. See [Bucket Naming](#bucket-naming) for details and usage.

**CIDR restrictions on buckets:** Most cloud object storage services support IP-based access restrictions. However, this is **not recommended** for kube-iam-assume buckets. Cloud provider STS services fetch the JWKS from a wide and unpublished range of IP addresses that vary by region and change over time. Applying CIDR restrictions to the bucket will cause STS to fail JWKS retrieval and break federation silently. If you require network-level controls, use the built-in HTTPS endpoint mode behind your own infrastructure instead.

### Signing Key Compromise

If the API server's SA signing key is compromised, an attacker could forge tokens for any service account. This risk is identical to managed Kubernetes. Mitigation: restrict control plane access, monitor cloud audit logs for anomalous `AssumeRoleWithWebIdentity` calls, and use kube-iam-assume's rotation support to roll to new keys.

---

## Roadmap

### v0.1 — The Bridge

- OIDC bridge controller with S3 publishing
- Automatic JWKS rotation with dual-publish overlap
- `kube-iam-assume generate-bucket-name` CLI for obfuscated bucket name generation (with tagging and ConfigMap/JSON/Helm output)
- `kube-iam-assume get-bucket-info` CLI for reverse-lookup of bucket metadata from ConfigMap or cloud tags
- `kube-iam-assume setup aws` CLI for one-time OIDC IdP registration
- `kube-iam-assume status` for sync health and diagnostics
- Helm chart
- AWS support

### v0.2 — Multi-Cloud

- GCS and Azure Blob publishing modes
- `kube-iam-assume setup gcp` and `kube-iam-assume setup azure` CLI commands
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

**Does kube-iam-assume replace SPIRE?**

No. SPIRE is a comprehensive workload identity framework for heterogeneous environments. kube-iam-assume solves one specific problem: making K8s service account tokens usable for cloud identity federation. If you only need cloud access from K8s, kube-iam-assume is simpler. If you need cross-platform identity, mTLS, or custom attestation, use SPIRE.

**Does kube-iam-assume require changes to my application code?**

No. The AWS, GCP, and Azure SDKs all support web identity token federation through their default credential chains. kube-iam-assume ensures the infrastructure prerequisite (reachable JWKS) is met. Your application code calls cloud APIs normally.

**What happens if kube-iam-assume goes down?**

Cloud providers cache the JWKS. If kube-iam-assume stops publishing, existing cached keys remain valid until the cache expires (varies by provider, typically hours). Existing pods with valid tokens continue to work. New pods can still obtain tokens — they just can't be validated if the JWKS disappears entirely. kube-iam-assume publishes to durable object storage, so even if the controller pod restarts, the published JWKS remains available.

**Can I use kube-iam-assume with EKS/GKE/AKS?**

You don't need to. These managed services already publish OIDC endpoints. kube-iam-assume is specifically for clusters where this is not provided.

**Does changing the service account issuer break anything?**

See [Issuer Configuration](#issuer-configuration) for a complete analysis. The short answer: it does not affect legacy SA tokens, internal K8s communication, or pod-to-API-server auth. It can affect systems that validate the `iss` claim of projected tokens. The API server supports multiple issuer values for safe migration.

**What Kubernetes versions are supported?**

Kubernetes 1.22 and above. Projected service account tokens (stable in 1.20) and service account issuer discovery (GA in 1.21) are both required.

**What about air-gapped environments?**

If your cluster has no internet egress at all, the cloud provider STS endpoint is also unreachable, so OIDC federation is not applicable. If your cluster has egress to cloud APIs but no inbound access, the object storage publishing mode works perfectly — kube-iam-assume pushes outbound to the bucket, cloud providers read from the bucket.

**How do I choose a bucket naming strategy?**

For development and testing, a manual name like `my-cluster-oidc` is fine. For production, use `kube-iam-assume generate-bucket-name --prefix=oidc --region=us-west-2` to generate a prefix + UUID name that is unguessable by scanners but still recognizable in your cloud console. See [Bucket Naming](#bucket-naming) for the full comparison.

**How is this different from just uploading JWKS to S3 manually?**

kube-iam-assume automates what you would otherwise do with a bash script, and adds: automatic key rotation detection and dual-publish handling, health checking, Prometheus metrics, CLI helpers for cloud provider registration, and distribution-specific documentation. If you rotate your SA signing keys and forget to update the bucket, federation breaks silently. kube-iam-assume prevents that.

---

## Contributing

We welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Areas where help is needed:

- GCP and Azure publishing mode implementation
- Testing on additional K8s distributions (OpenShift, MicroK8s, Charmed Kubernetes)
- Terraform modules for cloud-side setup
- Documentation improvements and translations

---

## Acknowledgements

Inspired by [amazon-eks-pod-identity-webhook](https://github.com/aws/amazon-eks-pod-identity-webhook), the project that pioneered IRSA on EKS and proved that Kubernetes service account tokens + OIDC federation is the right model for secretless cloud access. kube-iam-assume brings that same pattern to every cluster, not just managed ones.

---

## License

GNU General Public License v3.0 (GPL-3.0)
