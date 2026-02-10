// Package constants provides shared constants for the application.
package constants

const (
	// ControllerName is the name of the controller.
	ControllerName = "kube-iam-assume-controller"

	// DefaultNamespace is the default namespace for controller resources.
	DefaultNamespace = "kube-iam-assume-system"

	// DefaultLeaderElectionID is the default leader election lock name.
	DefaultLeaderElectionID = "kube-iam-assume-controller-leader-election"

	// DefaultRotationConfigMapName is the default name for the rotation state configmap.
	DefaultRotationConfigMapName = "kube-iam-assume-rotation-state"

	// DefaultOIDCConfigMapName is the default name for the OIDC metadata configmap.
	DefaultOIDCConfigMapName = "kube-iam-assume-oidc-metadata"

	// DefaultGCPWorkloadIdentityPoolID is the default ID for GCP Workload Identity Pool.
	DefaultGCPWorkloadIdentityPoolID = "kube-iam-assume-pool"

	// DefaultGCPWorkloadIdentityPoolProviderID is the default ID for GCP Workload Identity Pool Provider.
	DefaultGCPWorkloadIdentityPoolProviderID = "kube-iam-assume-provider"

	// ComponentNameOidcPoller is the component name for the OIDC poller.
	ComponentNameOidcPoller = "oidc-poller"

	// EventRecorderName is the name of the event recorder.
	EventRecorderName = "kube-iam-assume-controller"
)
