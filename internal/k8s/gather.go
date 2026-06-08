package k8s

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/duration"
)

// Ref identifies a target resource, e.g. {Kind: "deployment", Name: "api"}.
type Ref struct {
	Kind string
	Name string
}

func (r Ref) String() string { return r.Kind + "/" + r.Name }

var kindAliases = map[string]string{
	"po": "pod", "pod": "pod", "pods": "pod",
	"deploy": "deployment", "deployment": "deployment", "deployments": "deployment",
}

// ParseRef parses a "kind/name" reference such as "deployment/api".
func ParseRef(s string) (Ref, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Ref{}, fmt.Errorf("invalid resource %q: expected kind/name (e.g. deployment/api)", s)
	}
	kind, ok := kindAliases[strings.ToLower(parts[0])]
	if !ok {
		return Ref{}, fmt.Errorf("unsupported kind %q: supported kinds are pod, deployment", parts[0])
	}
	return Ref{Kind: kind, Name: parts[1]}, nil
}

// GatherOptions tune evidence collection.
type GatherOptions struct {
	Namespace string // empty = the client's resolved namespace
	TailLines int64  // log lines to fetch per container
}

// Gather collects status, events and logs for a resource into a text report
// suitable for feeding to an AI backend or printing as raw evidence.
func (c *Client) Gather(ctx context.Context, ref Ref, opts GatherOptions) (string, error) {
	ns := opts.Namespace
	if ns == "" {
		ns = c.Namespace
	}
	if opts.TailLines <= 0 {
		opts.TailLines = 100
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Kubernetes evidence: %s (namespace=%s, context=%s)\n\n", ref, ns, c.Context)

	switch ref.Kind {
	case "pod":
		pod, err := c.Clientset.CoreV1().Pods(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting pod: %w", err)
		}
		c.writePod(ctx, &b, pod, opts.TailLines)
	case "deployment":
		dep, err := c.Clientset.AppsV1().Deployments(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting deployment: %w", err)
		}
		c.writeDeployment(ctx, &b, dep, opts.TailLines)
	}

	return b.String(), nil
}

func (c *Client) writeDeployment(ctx context.Context, b *strings.Builder, dep *appsv1.Deployment, tail int64) {
	fmt.Fprintf(b, "## Deployment %s\n", dep.Name)
	fmt.Fprintf(b, "- Replicas: desired=%d ready=%d available=%d updated=%d\n",
		ptrInt32(dep.Spec.Replicas), dep.Status.ReadyReplicas, dep.Status.AvailableReplicas, dep.Status.UpdatedReplicas)
	fmt.Fprintf(b, "- Age: %s\n", age(dep.CreationTimestamp.Time))
	for _, cond := range dep.Status.Conditions {
		fmt.Fprintf(b, "- Condition %s=%s: %s (%s)\n", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	b.WriteString("\n")

	c.writeEvents(ctx, b, dep.Namespace, dep.Name)

	// Resolve the managed pods via the deployment's selector.
	sel := labels.Set(dep.Spec.Selector.MatchLabels).AsSelector().String()
	pods, err := c.Clientset.CoreV1().Pods(dep.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		fmt.Fprintf(b, "Could not list pods for deployment: %v\n", err)
		return
	}
	fmt.Fprintf(b, "## Managed pods (%d)\n\n", len(pods.Items))
	for i := range pods.Items {
		c.writePod(ctx, b, &pods.Items[i], tail)
	}
}

func (c *Client) writePod(ctx context.Context, b *strings.Builder, pod *corev1.Pod, tail int64) {
	fmt.Fprintf(b, "### Pod %s\n", pod.Name)
	fmt.Fprintf(b, "- Phase: %s | Node: %s | Age: %s\n", pod.Status.Phase, pod.Spec.NodeName, age(pod.CreationTimestamp.Time))
	if pod.Status.Reason != "" {
		fmt.Fprintf(b, "- Reason: %s %s\n", pod.Status.Reason, pod.Status.Message)
	}

	for _, cs := range pod.Status.ContainerStatuses {
		fmt.Fprintf(b, "- Container %s: ready=%t restarts=%d state=%s\n",
			cs.Name, cs.Ready, cs.RestartCount, containerState(cs.State))
		if cs.LastTerminationState.Terminated != nil {
			t := cs.LastTerminationState.Terminated
			fmt.Fprintf(b, "  last termination: reason=%s exit=%d signal=%d\n", t.Reason, t.ExitCode, t.Signal)
		}
	}

	// Resource requests/limits help spot OOM / throttling causes.
	for _, ct := range pod.Spec.Containers {
		req, lim := ct.Resources.Requests, ct.Resources.Limits
		if len(req) > 0 || len(lim) > 0 {
			fmt.Fprintf(b, "- Resources %s: requests=%s limits=%s\n", ct.Name, resList(req), resList(lim))
		}
	}
	b.WriteString("\n")

	c.writeEvents(ctx, b, pod.Namespace, pod.Name)
	c.writeLogs(ctx, b, pod, tail)
	b.WriteString("\n")
}

func (c *Client) writeEvents(ctx context.Context, b *strings.Builder, ns, name string) {
	events, err := c.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.name", name).String(),
	})
	if err != nil || len(events.Items) == 0 {
		return
	}
	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.Before(&events.Items[j].LastTimestamp)
	})
	fmt.Fprintf(b, "Events for %s:\n", name)
	for _, e := range events.Items {
		fmt.Fprintf(b, "  [%s] %s x%d: %s\n", e.Type, e.Reason, e.Count, strings.TrimSpace(e.Message))
	}
	b.WriteString("\n")
}

func (c *Client) writeLogs(ctx context.Context, b *strings.Builder, pod *corev1.Pod, tail int64) {
	for _, ct := range pod.Spec.Containers {
		// Current logs.
		c.dumpLog(ctx, b, pod, ct.Name, tail, false)
		// If the container has restarted, the previous logs usually hold the cause.
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name == ct.Name && cs.RestartCount > 0 {
				c.dumpLog(ctx, b, pod, ct.Name, tail, true)
			}
		}
	}
}

func (c *Client) dumpLog(ctx context.Context, b *strings.Builder, pod *corev1.Pod, container string, tail int64, previous bool) {
	req := c.Clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tail,
		Previous:  previous,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil || len(data) == 0 {
		return
	}
	label := "logs"
	if previous {
		label = "previous logs"
	}
	fmt.Fprintf(b, "```\n%s of %s/%s:\n%s\n```\n", label, pod.Name, container, strings.TrimSpace(string(data)))
}

// --- small helpers ---

func age(t time.Time) string { return duration.HumanDuration(time.Since(t)) }

func ptrInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func containerState(s corev1.ContainerState) string {
	switch {
	case s.Waiting != nil:
		return fmt.Sprintf("Waiting(%s: %s)", s.Waiting.Reason, strings.TrimSpace(s.Waiting.Message))
	case s.Terminated != nil:
		return fmt.Sprintf("Terminated(%s exit=%d)", s.Terminated.Reason, s.Terminated.ExitCode)
	case s.Running != nil:
		return "Running"
	default:
		return "Unknown"
	}
}

func resList(rl corev1.ResourceList) string {
	if len(rl) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(rl))
	for k, v := range rl {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v.String()))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
