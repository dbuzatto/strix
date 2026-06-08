package k8s

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Usage is a CPU/memory usage sample for a node or pod, with totals where known.
type Usage struct {
	Name      string
	Namespace string // empty for nodes
	CPUUsed   resource.Quantity
	CPUTotal  resource.Quantity // allocatable; zero for pods
	MemUsed   resource.Quantity
	MemTotal  resource.Quantity // allocatable; zero for pods
}

// CPUPercent returns CPU usage as a percentage of the total, or -1 if unknown.
func (u Usage) CPUPercent() float64 { return percent(u.CPUUsed.MilliValue(), u.CPUTotal.MilliValue()) }

// MemPercent returns memory usage as a percentage of the total, or -1 if unknown.
func (u Usage) MemPercent() float64 { return percent(u.MemUsed.Value(), u.MemTotal.Value()) }

func percent(used, total int64) float64 {
	if total <= 0 {
		return -1
	}
	return float64(used) / float64(total) * 100
}

// NodeUsage returns per-node CPU/memory usage joined with node allocatable.
func (c *Client) NodeUsage(ctx context.Context) ([]Usage, error) {
	metrics, err := c.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, metricsErr(err)
	}

	nodes, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	allocatable := make(map[string]corev1.ResourceList, len(nodes.Items))
	for i := range nodes.Items {
		allocatable[nodes.Items[i].Name] = nodes.Items[i].Status.Allocatable
	}

	out := make([]Usage, 0, len(metrics.Items))
	for _, m := range metrics.Items {
		u := Usage{
			Name:    m.Name,
			CPUUsed: m.Usage[corev1.ResourceCPU],
			MemUsed: m.Usage[corev1.ResourceMemory],
		}
		if alloc, ok := allocatable[m.Name]; ok {
			u.CPUTotal = alloc[corev1.ResourceCPU]
			u.MemTotal = alloc[corev1.ResourceMemory]
		}
		out = append(out, u)
	}
	sortByCPU(out)
	return out, nil
}

// PodUsage returns per-pod CPU/memory usage (summed across containers) in ns,
// optionally filtered by a label selector ("" = no filter). An empty ns means
// all namespaces. Pod resource limits are joined in when available so usage can
// be shown as a percentage.
func (c *Client) PodUsage(ctx context.Context, ns, selector string) ([]Usage, error) {
	listOpts := metav1.ListOptions{LabelSelector: selector}

	metrics, err := c.Metrics.MetricsV1beta1().PodMetricses(ns).List(ctx, listOpts)
	if err != nil {
		return nil, metricsErr(err)
	}

	// Best-effort: learn each pod's limits so we can render a percentage gauge.
	cpuLim := map[string]resource.Quantity{}
	memLim := map[string]resource.Quantity{}
	if pods, perr := c.Clientset.CoreV1().Pods(ns).List(ctx, listOpts); perr == nil {
		for i := range pods.Items {
			p := &pods.Items[i]
			k := p.Namespace + "/" + p.Name
			if cpu, ok := sumLimit(p, corev1.ResourceCPU); ok {
				cpuLim[k] = cpu
			}
			if mem, ok := sumLimit(p, corev1.ResourceMemory); ok {
				memLim[k] = mem
			}
		}
	}

	out := make([]Usage, 0, len(metrics.Items))
	for _, m := range metrics.Items {
		var cpu, mem resource.Quantity
		for _, ct := range m.Containers {
			cpu.Add(ct.Usage[corev1.ResourceCPU])
			mem.Add(ct.Usage[corev1.ResourceMemory])
		}
		k := m.Namespace + "/" + m.Name
		out = append(out, Usage{
			Name:      m.Name,
			Namespace: m.Namespace,
			CPUUsed:   cpu,
			MemUsed:   mem,
			CPUTotal:  cpuLim[k],
			MemTotal:  memLim[k],
		})
	}
	sortByCPU(out)
	return out, nil
}

// DeploymentSelector returns the label selector of a deployment as a string,
// suitable for filtering its pods.
func (c *Client) DeploymentSelector(ctx context.Context, ns, name string) (string, error) {
	dep, err := c.Clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting deployment: %w", err)
	}
	return labels.Set(dep.Spec.Selector.MatchLabels).AsSelector().String(), nil
}

// sumLimit sums a resource limit across all containers. ok is false when any
// container lacks the limit, since the pod could then exceed the sum.
func sumLimit(p *corev1.Pod, name corev1.ResourceName) (resource.Quantity, bool) {
	if len(p.Spec.Containers) == 0 {
		return resource.Quantity{}, false
	}
	var total resource.Quantity
	for _, ct := range p.Spec.Containers {
		q, ok := ct.Resources.Limits[name]
		if !ok || q.IsZero() {
			return resource.Quantity{}, false
		}
		total.Add(q)
	}
	return total, true
}

func sortByCPU(u []Usage) {
	sort.Slice(u, func(i, j int) bool { return u[i].CPUUsed.MilliValue() > u[j].CPUUsed.MilliValue() })
}

// metricsErr turns the "metrics.k8s.io API not available" case into actionable
// guidance instead of an opaque NotFound.
func metricsErr(err error) error {
	if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
		return fmt.Errorf("metrics-server is not available in this cluster: install it (https://github.com/kubernetes-sigs/metrics-server) to use 'strix top'")
	}
	return fmt.Errorf("fetching metrics: %w", err)
}
