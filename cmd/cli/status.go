package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/hixichen/kube-iam-assume/pkg/constants"
	"github.com/hixichen/kube-iam-assume/pkg/rotation"
)

// NewStatusCommand creates the status command.
func NewStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show KubeAssume status",
		Long:  `Shows the current status of KubeAssume including sync health, published keys, and rotation status.`,
		RunE:  runStatus,
	}
	return cmd
}

// StatusInfo holds status information for display.
type StatusInfo struct {
	// Controller status
	ControllerRunning   bool
	ControllerName      string
	ControllerNamespace string

	// Sync status
	LastSyncTime    time.Time
	LastSyncSuccess bool
	LastSyncError   string

	// Keys status
	PublishedKeyCount int
	ActiveKeyIDs      []string

	// Rotation status
	RotationActive     bool
	OverlapEndsAt      time.Time
	KeysPendingRemoval int

	// OIDC provider status
	OIDCProviderARN string
	IssuerURL       string
}

// StatusChecker retrieves status information.
type StatusChecker struct {
	kubeconfig string
	namespace  string
}

// NewStatusChecker creates a new StatusChecker.
func NewStatusChecker(kubeconfig, namespace string) *StatusChecker {
	return &StatusChecker{
		kubeconfig: kubeconfig,
		namespace:  namespace,
	}
}

// GetStatus retrieves the current status.
func (c *StatusChecker) GetStatus(ctx context.Context) (*StatusInfo, error) {
	// Build kubernetes client
	clientset, err := c.buildClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to cluster: %w", err)
	}

	info := &StatusInfo{
		ControllerNamespace: c.namespace,
	}

	// Find kubeassume controller deployment
	deployments, err := clientset.AppsV1().Deployments(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=kube-iam-assume",
	})
	if err != nil {
		return info, fmt.Errorf("failed to list deployments: %w", err)
	}

	if len(deployments.Items) > 0 {
		deploy := deployments.Items[0]
		info.ControllerName = deploy.Name
		info.ControllerRunning = deploy.Status.ReadyReplicas > 0
	}

	// Read rotation state ConfigMap
	cm, err := clientset.CoreV1().ConfigMaps(c.namespace).Get(ctx, "kube-iam-assume-rotation-state", metav1.GetOptions{})
	if err == nil {
		stateData, ok := cm.Data["state"]
		if ok {
			var state rotation.State
			if jsonErr := json.Unmarshal([]byte(stateData), &state); jsonErr == nil {
				info.LastSyncTime = state.LastUpdated

				// Count keys and detect rotation
				for keyID, keyState := range state.Keys {
					info.ActiveKeyIDs = append(info.ActiveKeyIDs, keyID)
					if keyState.MarkedForRemoval != nil {
						info.RotationActive = true
						info.KeysPendingRemoval++
					}
				}
				info.PublishedKeyCount = len(state.Keys)
				info.LastSyncSuccess = true
			}
		}
	}

	return info, nil
}

// buildClient creates a kubernetes clientset.
func (c *StatusChecker) buildClient() (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if c.kubeconfig != "" {
		loadingRules.ExplicitPath = c.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build kube config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return clientset, nil
}

// PrintStatus prints the status in a formatted way.
func PrintStatus(info *StatusInfo) {
	fmt.Println("KubeAssume Status")
	fmt.Println("=================")
	fmt.Println()

	// Controller status
	controllerStatus := "Not Running"
	if info.ControllerRunning {
		controllerStatus = "Running"
	}
	fmt.Printf("Controller:      %s (%s/%s)\n",
		controllerStatus, info.ControllerNamespace, info.ControllerName)

	// Last sync
	syncStatus := "success"
	if !info.LastSyncSuccess {
		syncStatus = fmt.Sprintf("error: %s", info.LastSyncError)
	}
	timeSince := formatTimeSince(info.LastSyncTime)
	fmt.Printf("Last Sync:       %s (%s)\n", timeSince, syncStatus)

	// Published keys
	fmt.Printf("Published Keys:  %d\n", info.PublishedKeyCount)

	// Key IDs
	if len(info.ActiveKeyIDs) > 0 {
		fmt.Printf("Key IDs:         ")
		for i, kid := range info.ActiveKeyIDs {
			if i > 0 {
				fmt.Printf(", ")
			}
			// Truncate long key IDs for display
			if len(kid) > 16 {
				fmt.Printf("%s...", kid[:16])
			} else {
				fmt.Printf("%s", kid)
			}
		}
		fmt.Println()
	}

	// Rotation status
	if info.RotationActive {
		fmt.Printf("Rotation:        Active (%d keys pending removal)\n", info.KeysPendingRemoval)
	} else {
		fmt.Println("Rotation:        None")
	}

	// OIDC provider
	if info.OIDCProviderARN != "" {
		fmt.Printf("OIDC Provider:   %s\n", info.OIDCProviderARN)
	}
	if info.IssuerURL != "" {
		fmt.Printf("Issuer URL:      %s\n", info.IssuerURL)
	}

	fmt.Println()
}

// formatTimeSince formats a time as "X minutes ago".
func formatTimeSince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	d := time.Since(t)

	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Create status checker
	checker := NewStatusChecker("", constants.DefaultNamespace) // TODO: make namespace configurable

	// Get status
	info, err := checker.GetStatus(ctx)
	if err != nil {
		// Print partial status even if there's an error
		fmt.Fprintf(os.Stderr, "Warning: Could not retrieve full status: %v\n", err)
		info = &StatusInfo{}
	}

	// Display formatted status
	PrintStatus(info)

	return nil
}
