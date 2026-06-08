// Package k8s wraps client-go to connect to a cluster using the same
// kubeconfig discovery rules as kubectl.
package k8s

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Client is a connected Kubernetes client plus the resolved context/namespace.
type Client struct {
	Clientset kubernetes.Interface
	Namespace string
	Context   string
}

// Options carry CLI overrides for connecting to a cluster.
type Options struct {
	Kubeconfig string // explicit path; overrides $KUBECONFIG and ~/.kube/config
	Context    string // kubeconfig context to use; empty = current-context
	Namespace  string // namespace scope; empty = namespace from the context
}

// New builds a Kubernetes client using the same discovery order as kubectl:
//  1. the --kubeconfig flag (opts.Kubeconfig)
//  2. the $KUBECONFIG environment variable (colon-separated paths)
//  3. ~/.kube/config
func New(opts Options) (*Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loadingRules.ExplicitPath = opts.Kubeconfig
	}

	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}

	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)

	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("creating clientset: %w", err)
	}

	ns := opts.Namespace
	if ns == "" {
		// Resolve the namespace bound to the active context; fall back to "default".
		if resolved, _, nerr := cc.Namespace(); nerr == nil && resolved != "" {
			ns = resolved
		} else {
			ns = "default"
		}
	}

	ctxName := opts.Context
	if ctxName == "" {
		if raw, rerr := cc.RawConfig(); rerr == nil {
			ctxName = raw.CurrentContext
		}
	}

	return &Client{Clientset: clientset, Namespace: ns, Context: ctxName}, nil
}
