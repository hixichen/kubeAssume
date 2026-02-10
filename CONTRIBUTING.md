# Contributing to kube-iam-assume

Thank you for your interest in kube-iam-assume. This document provides guidelines and information for contributors.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How to Contribute](#how-to-contribute)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Making Changes](#making-changes)
- [Pull Request Process](#pull-request-process)
- [Coding Guidelines](#coding-guidelines)
- [Testing](#testing)
- [Documentation](#documentation)
- [Issue Guidelines](#issue-guidelines)
- [Areas Where Help Is Needed](#areas-where-help-is-needed)
- [License](#license)

---

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior by opening an issue.

---

## How to Contribute

There are many ways to contribute to kube-iam-assume beyond writing code:

- **Report bugs** — file an issue describing what you expected and what actually happened
- **Suggest features** — open an issue describing the use case and why it matters
- **Improve documentation** — fix typos, clarify explanations, add examples
- **Test on your distribution** — try kube-iam-assume on your K8s distribution and report results
- **Review pull requests** — provide feedback on open PRs
- **Write tests** — expand unit and integration test coverage
- **Share your experience** — blog posts, talks, and tutorials help the community grow

---

## Development Setup

### Prerequisites

- Go 1.22+
- Docker
- kubectl with access to a Kubernetes cluster (kind or minikube is fine for development)
- Helm 3
- An AWS account (for testing S3 publishing mode)

### Clone and Build

```bash
git clone https://github.com/kube-iam-assume/kube-iam-assume.git
cd kube-iam-assume
make build
```

### Run Tests

```bash
make test          # unit tests
make test-e2e      # end-to-end tests (requires a running cluster)
make lint          # run linters
```

### Run Locally

For development, you can run the controller outside the cluster:

```bash
# make sure your kubeconfig points to a test cluster
make run
```

### Build Container Image

```bash
make docker-build IMG=kube-iam-assume:dev
```

### Deploy to a Test Cluster

```bash
make docker-build IMG=kube-iam-assume:dev
kind load docker-image kube-iam-assume:dev    # if using kind
make deploy IMG=kube-iam-assume:dev
```

---

## Project Structure

```
kube-iam-assume/
├── cmd/
│   ├── controller/        # OIDC bridge controller entrypoint
│   └── cli/               # kube-iam-assume CLI entrypoint
├── pkg/
│   ├── bridge/            # OIDC discovery + JWKS fetching logic
│   ├── naming/            # Bucket name generation strategies (prefix + UUID)
│   ├── publisher/         # Publishing backends (S3, GCS, Azure Blob, HTTPS)
│   ├── rotation/          # Key rotation detection and dual-publish logic
│   ├── health/            # Health check and status reporting
│   └── metrics/           # Prometheus metrics
├── deploy/
│   └── helm/              # Helm chart
├── hack/                  # Development scripts and tools
├── docs/                  # Additional documentation
├── test/
│   ├── unit/
│   └── e2e/
├── Makefile
├── Dockerfile
├── README.md
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
└── LICENSE
```

---

## Making Changes

### Before You Start

1. **Check existing issues and PRs.** Someone may already be working on what you have in mind. If you find a related issue, comment on it to let others know you are working on it.

2. **Open an issue first for significant changes.** Bug fixes and small improvements can go straight to a PR. New features, architectural changes, or anything that changes the public API should start with an issue for discussion.

3. **One concern per PR.** Keep pull requests focused. A PR that fixes a bug should not also refactor unrelated code. This makes reviews faster and history cleaner.

### Branch Naming

Use descriptive branch names:

```
fix/rotation-overlap-off-by-one
feat/gcs-publisher
docs/kind-setup-guide
test/s3-publisher-unit-tests
```

### Commit Messages

Write clear, concise commit messages. Use the following format:

```
component: short description of the change

Longer explanation if needed. Explain the motivation for the change,
what it does, and any trade-offs or decisions made.

Fixes #123
```

Examples:

```
publisher/s3: handle bucket region redirect on first sync
rotation: fix overlap period calculation for sub-hour durations
docs: add RKE2 issuer configuration example
```

Keep the first line under 72 characters. Reference related issues where applicable.

---

## Pull Request Process

1. **Fork the repository** and create your branch from `main`.

2. **Write or update tests** for your changes. PRs that add new functionality should include tests. PRs that fix bugs should include a test that reproduces the bug.

3. **Run the full test suite** before submitting:
   ```bash
   make test
   make lint
   ```

4. **Update documentation** if your change affects user-facing behavior. This includes the README, Helm chart values, and CLI help text.

5. **Open the PR** with a clear description of what the change does and why. Fill out the PR template.

6. **Respond to review feedback.** Make requested changes in new commits so reviewers can see the diff. Squash before merge if the commit history is noisy.

7. **Be patient.** This is a small project and reviews may take a few days. If your PR has not received feedback within a week, leave a polite comment.

### PR Checklist

- [ ] Code compiles without errors (`make build`)
- [ ] All tests pass (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] New functionality includes tests
- [ ] Documentation updated if needed
- [ ] Commit messages follow the convention
- [ ] PR description explains the change and links to any related issues

---

## Coding Guidelines

### Language

kube-iam-assume is written in Go. We follow standard Go conventions:

- Run `gofmt` and `goimports` on all code
- Follow [Effective Go](https://go.dev/doc/effective_go)
- Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

### Error Handling

- Always handle errors. Do not use `_` to discard errors unless there is a comment explaining why.
- Wrap errors with context using `fmt.Errorf("doing something: %w", err)`.
- Use structured logging (slog) with appropriate log levels.

### Dependencies

- Minimize external dependencies. Every dependency is an ongoing maintenance cost.
- Use the standard library where possible.
- controller-runtime (kubebuilder) is the framework for the K8s controller.
- Cloud SDK dependencies (aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go) are acceptable for their respective publisher backends.

### Security

This is a security-sensitive project. Keep the following in mind:

- Never log token contents, signing keys, or credentials.
- Validate all inputs, especially URLs and bucket names.
- Use the principle of least privilege for RBAC and IAM configurations.
- If you find a security vulnerability, please report it privately by emailing the maintainers. Do not open a public issue.

---

## Testing

### Unit Tests

Unit tests live alongside the code they test (`*_test.go` files). They should:

- Be fast (no network calls, no cluster access)
- Use table-driven tests where appropriate
- Mock external dependencies (S3 client, K8s API)

```bash
make test
```

### End-to-End Tests

E2E tests run against a real Kubernetes cluster and verify the full flow:

- Controller starts and publishes OIDC metadata
- JWKS rotation is detected and dual-published
- Published metadata is valid and parseable

```bash
make test-e2e
```

E2E tests require a running cluster. The test suite creates and cleans up its own resources.

### Testing on Different Distributions

We especially value test reports from distributions we don't test in CI. If you run kube-iam-assume on any of the following and can confirm it works (or find issues), please open an issue or PR:

- kubeadm
- k3s / k3d
- RKE2
- Talos
- MicroK8s
- Charmed Kubernetes
- OpenShift
- kind (used in CI)
- minikube

---

## Documentation

Documentation changes are contributions too. If you notice something that is unclear, incorrect, or missing, please submit a PR.

### What to document

- New features or configuration options
- Distribution-specific setup instructions
- Troubleshooting steps you discovered while using kube-iam-assume
- Architecture decisions (add to `docs/architecture/` as ADRs)

### Style

- Use plain, direct language. Avoid jargon where possible.
- Show concrete examples. A code block or command is worth more than a paragraph of explanation.
- Keep the README focused. Detailed guides belong in `docs/`.

---

## Issue Guidelines

### Bug Reports

Please include:

- kube-iam-assume version
- Kubernetes version and distribution
- Publishing mode (S3, GCS, Azure Blob, HTTPS)
- Steps to reproduce
- Expected behavior
- Actual behavior
- Controller logs (with any sensitive information redacted)

### Feature Requests

Please include:

- The problem you are trying to solve (not just the solution you have in mind)
- Your current workaround, if any
- How this would benefit other users

### Labels

- `good-first-issue` — suitable for new contributors
- `help-wanted` — maintainers would appreciate help with this
- `bug` — confirmed bug
- `enhancement` — feature request or improvement
- `documentation` — documentation-only change
- `question` — needs discussion before implementation

---

## Areas Where Help Is Needed

We are a small project and welcome contributions in these areas:

**Publishing backends**
- Google Cloud Storage publisher implementation
- Azure Blob Storage publisher implementation

**Cloud provider setup**
- `kube-iam-assume setup gcp` CLI command
- `kube-iam-assume setup azure` CLI command
- Terraform modules for OIDC IdP registration (AWS, GCP, Azure)

**Testing**
- Unit tests for publisher backends
- E2E test framework
- Testing on K8s distributions beyond kind

**Documentation**
- Distribution-specific issuer configuration guides
- Troubleshooting guide
- Architecture Decision Records

**Observability**
- Prometheus metrics implementation
- Grafana dashboard template

If you want to work on any of these, please open an issue or comment on an existing one so we can coordinate.

---

## License

By contributing to kube-iam-assume, you agree that your contributions will be licensed under the [GNU General Public License v3.0 (GPL-3.0)](LICENSE).
