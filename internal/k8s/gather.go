package k8s

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
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
	"sts": "statefulset", "statefulset": "statefulset", "statefulsets": "statefulset",
	"ds": "daemonset", "daemonset": "daemonset", "daemonsets": "daemonset",
	"job": "job", "jobs": "job",
	"cj": "cronjob", "cronjob": "cronjob", "cronjobs": "cronjob",
	"no": "node", "node": "node", "nodes": "node",
}

// supportedKinds lists the canonical kinds Gather understands, for error messages.
const supportedKinds = "pod, deployment, statefulset, daemonset, job, cronjob, node"

// ParseRef parses a "kind/name" reference such as "deployment/api".
func ParseRef(s string) (Ref, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Ref{}, fmt.Errorf("invalid resource %q: expected kind/name (e.g. deployment/api)", s)
	}
	kind, ok := kindAliases[strings.ToLower(parts[0])]
	if !ok {
		return Ref{}, fmt.Errorf("unsupported kind %q: supported kinds are %s", parts[0], supportedKinds)
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
	if ref.Kind == "node" {
		fmt.Fprintf(&b, "# Kubernetes evidence: %s (context=%s)\n\n", ref, c.Context)
	} else {
		fmt.Fprintf(&b, "# Kubernetes evidence: %s (namespace=%s, context=%s)\n\n", ref, ns, c.Context)
	}

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
	case "statefulset":
		sts, err := c.Clientset.AppsV1().StatefulSets(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting statefulset: %w", err)
		}
		c.writeStatefulSet(ctx, &b, sts, opts.TailLines)
	case "daemonset":
		ds, err := c.Clientset.AppsV1().DaemonSets(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting daemonset: %w", err)
		}
		c.writeDaemonSet(ctx, &b, ds, opts.TailLines)
	case "job":
		job, err := c.Clientset.BatchV1().Jobs(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting job: %w", err)
		}
		c.writeJob(ctx, &b, job, opts.TailLines)
	case "cronjob":
		cj, err := c.Clientset.BatchV1().CronJobs(ns).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting cronjob: %w", err)
		}
		c.writeCronJob(ctx, &b, cj, opts.TailLines)
	case "node":
		node, err := c.Clientset.CoreV1().Nodes().Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("getting node: %w", err)
		}
		c.writeNode(ctx, &b, node)
	default:
		return "", fmt.Errorf("unsupported kind %q: supported kinds are %s", ref.Kind, supportedKinds)
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
	c.writeManagedPods(ctx, b, dep.Namespace, dep.Spec.Selector, tail)
}

func (c *Client) writeStatefulSet(ctx context.Context, b *strings.Builder, sts *appsv1.StatefulSet, tail int64) {
	fmt.Fprintf(b, "## StatefulSet %s\n", sts.Name)
	fmt.Fprintf(b, "- Replicas: desired=%d ready=%d current=%d updated=%d\n",
		ptrInt32(sts.Spec.Replicas), sts.Status.ReadyReplicas, sts.Status.CurrentReplicas, sts.Status.UpdatedReplicas)
	fmt.Fprintf(b, "- Revisions: current=%s update=%s\n", sts.Status.CurrentRevision, sts.Status.UpdateRevision)
	fmt.Fprintf(b, "- Age: %s\n", age(sts.CreationTimestamp.Time))
	for _, cond := range sts.Status.Conditions {
		fmt.Fprintf(b, "- Condition %s=%s: %s (%s)\n", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	b.WriteString("\n")

	c.writeEvents(ctx, b, sts.Namespace, sts.Name)
	c.writeManagedPods(ctx, b, sts.Namespace, sts.Spec.Selector, tail)
}

func (c *Client) writeDaemonSet(ctx context.Context, b *strings.Builder, ds *appsv1.DaemonSet, tail int64) {
	fmt.Fprintf(b, "## DaemonSet %s\n", ds.Name)
	fmt.Fprintf(b, "- Scheduling: desired=%d current=%d ready=%d available=%d unavailable=%d misscheduled=%d\n",
		ds.Status.DesiredNumberScheduled, ds.Status.CurrentNumberScheduled, ds.Status.NumberReady,
		ds.Status.NumberAvailable, ds.Status.NumberUnavailable, ds.Status.NumberMisscheduled)
	fmt.Fprintf(b, "- Age: %s\n", age(ds.CreationTimestamp.Time))
	for _, cond := range ds.Status.Conditions {
		fmt.Fprintf(b, "- Condition %s=%s: %s (%s)\n", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	b.WriteString("\n")

	c.writeEvents(ctx, b, ds.Namespace, ds.Name)
	c.writeManagedPods(ctx, b, ds.Namespace, ds.Spec.Selector, tail)
}

// writeManagedPods lists and dumps the pods matched by a workload's selector.
func (c *Client) writeManagedPods(ctx context.Context, b *strings.Builder, ns string, selector *metav1.LabelSelector, tail int64) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		fmt.Fprintf(b, "Could not build pod selector: %v\n", err)
		return
	}
	pods, err := c.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		fmt.Fprintf(b, "Could not list managed pods: %v\n", err)
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

	for _, cs := range pod.Status.InitContainerStatuses {
		fmt.Fprintf(b, "- Init container %s: ready=%t restarts=%d state=%s\n",
			cs.Name, cs.Ready, cs.RestartCount, containerState(cs.State))
		if cs.LastTerminationState.Terminated != nil {
			t := cs.LastTerminationState.Terminated
			fmt.Fprintf(b, "  last termination: reason=%s exit=%d signal=%d\n", t.Reason, t.ExitCode, t.Signal)
		}
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

func (c *Client) writeJob(ctx context.Context, b *strings.Builder, job *batchv1.Job, tail int64) {
	fmt.Fprintf(b, "## Job %s\n", job.Name)
	fmt.Fprintf(b, "- Pods: active=%d succeeded=%d failed=%d\n", job.Status.Active, job.Status.Succeeded, job.Status.Failed)
	fmt.Fprintf(b, "- Spec: completions=%d parallelism=%d backoffLimit=%d\n",
		ptrInt32(job.Spec.Completions), ptrInt32(job.Spec.Parallelism), ptrInt32(job.Spec.BackoffLimit))
	if job.Spec.ActiveDeadlineSeconds != nil {
		fmt.Fprintf(b, "- ActiveDeadlineSeconds: %d\n", *job.Spec.ActiveDeadlineSeconds)
	}
	if job.Status.StartTime != nil {
		fmt.Fprintf(b, "- Started: %s ago\n", age(job.Status.StartTime.Time))
	}
	if job.Status.CompletionTime != nil {
		fmt.Fprintf(b, "- Completed: %s ago\n", age(job.Status.CompletionTime.Time))
	}
	for _, cond := range job.Status.Conditions {
		fmt.Fprintf(b, "- Condition %s=%s: %s (%s)\n", cond.Type, cond.Status, cond.Reason, strings.TrimSpace(cond.Message))
	}
	b.WriteString("\n")

	c.writeEvents(ctx, b, job.Namespace, job.Name)
	c.writeManagedPods(ctx, b, job.Namespace, job.Spec.Selector, tail)
}

func (c *Client) writeCronJob(ctx context.Context, b *strings.Builder, cj *batchv1.CronJob, tail int64) {
	fmt.Fprintf(b, "## CronJob %s\n", cj.Name)
	fmt.Fprintf(b, "- Schedule: %q | Suspended: %t | ConcurrencyPolicy: %s\n",
		cj.Spec.Schedule, ptrBool(cj.Spec.Suspend), cj.Spec.ConcurrencyPolicy)
	fmt.Fprintf(b, "- Active jobs: %d\n", len(cj.Status.Active))
	if cj.Status.LastScheduleTime != nil {
		fmt.Fprintf(b, "- Last scheduled: %s ago\n", age(cj.Status.LastScheduleTime.Time))
	}
	if cj.Status.LastSuccessfulTime != nil {
		fmt.Fprintf(b, "- Last successful: %s ago\n", age(cj.Status.LastSuccessfulTime.Time))
	}
	fmt.Fprintf(b, "- Age: %s\n", age(cj.CreationTimestamp.Time))
	b.WriteString("\n")

	c.writeEvents(ctx, b, cj.Namespace, cj.Name)

	// Drill into the most recent jobs this CronJob spawned — their pods and logs
	// usually hold the reason a scheduled run failed.
	jobs, err := c.Clientset.BatchV1().Jobs(cj.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(b, "Could not list jobs for cronjob: %v\n", err)
		return
	}
	var owned []batchv1.Job
	for i := range jobs.Items {
		for _, ref := range jobs.Items[i].OwnerReferences {
			if ref.UID == cj.UID {
				owned = append(owned, jobs.Items[i])
				break
			}
		}
	}
	sort.Slice(owned, func(i, j int) bool {
		return owned[i].CreationTimestamp.After(owned[j].CreationTimestamp.Time)
	})
	const maxJobs = 3
	if len(owned) > maxJobs {
		owned = owned[:maxJobs]
	}
	fmt.Fprintf(b, "## Recent jobs (%d shown)\n\n", len(owned))
	for i := range owned {
		c.writeJob(ctx, b, &owned[i], tail)
	}
}

func (c *Client) writeNode(ctx context.Context, b *strings.Builder, node *corev1.Node) {
	fmt.Fprintf(b, "## Node %s\n", node.Name)
	fmt.Fprintf(b, "- Age: %s | Unschedulable: %t\n", age(node.CreationTimestamp.Time), node.Spec.Unschedulable)
	ni := node.Status.NodeInfo
	fmt.Fprintf(b, "- Kubelet: %s | OS: %s/%s | Runtime: %s | Kernel: %s\n",
		ni.KubeletVersion, ni.OperatingSystem, ni.Architecture, ni.ContainerRuntimeVersion, ni.KernelVersion)
	fmt.Fprintf(b, "- Capacity: cpu=%s memory=%s pods=%s\n",
		node.Status.Capacity.Cpu(), node.Status.Capacity.Memory(), node.Status.Capacity.Pods())
	fmt.Fprintf(b, "- Allocatable: cpu=%s memory=%s pods=%s\n",
		node.Status.Allocatable.Cpu(), node.Status.Allocatable.Memory(), node.Status.Allocatable.Pods())
	for _, cond := range node.Status.Conditions {
		fmt.Fprintf(b, "- Condition %s=%s: %s (%s)\n", cond.Type, cond.Status, cond.Reason, strings.TrimSpace(cond.Message))
	}
	for _, t := range node.Spec.Taints {
		fmt.Fprintf(b, "- Taint: %s=%s:%s\n", t.Key, t.Value, t.Effect)
	}
	b.WriteString("\n")

	c.writeEvents(ctx, b, "", node.Name)

	// Summarize the pods scheduled on this node, calling out the unhealthy ones.
	pods, err := c.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node.Name).String(),
	})
	if err != nil {
		fmt.Fprintf(b, "Could not list pods on node: %v\n", err)
		return
	}
	var unhealthy []string
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodSucceeded {
			unhealthy = append(unhealthy, fmt.Sprintf("%s/%s (%s)", p.Namespace, p.Name, p.Status.Phase))
		}
	}
	fmt.Fprintf(b, "## Pods on node: %d total, %d not Running/Succeeded\n", len(pods.Items), len(unhealthy))
	for _, u := range unhealthy {
		fmt.Fprintf(b, "- %s\n", u)
	}
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
	// Init containers come first: when one of them is stuck the main containers
	// never start, so the init logs are the only place the cause can be.
	containers := make([]corev1.Container, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	containers = append(containers, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, ct := range containers {
		// Current logs.
		c.dumpLog(ctx, b, pod, ct.Name, tail, false)
		// If the container has restarted, the previous logs usually hold the cause.
		for _, cs := range allContainerStatuses(pod) {
			if cs.Name == ct.Name && cs.RestartCount > 0 {
				c.dumpLog(ctx, b, pod, ct.Name, tail, true)
			}
		}
	}
}

// maxLogBytes caps how much of each container log enters the evidence. Logs
// dominate prompt size and a single verbose JSON line can be kilobytes, so even
// with a line-based --tail the total can overflow the model's context. We keep
// only the most recent maxLogBytes — the tail is where the root cause lives.
const maxLogBytes = 16 * 1024

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
	text := strings.TrimSpace(string(data))
	note := ""
	if len(text) > maxLogBytes {
		// Keep the tail (most recent, most relevant) and trim forward to the
		// next line boundary so the dump doesn't start mid-line.
		text = text[len(text)-maxLogBytes:]
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
		note = fmt.Sprintf(" (truncated to last ~%dKB)", maxLogBytes/1024)
	}
	fmt.Fprintf(b, "```\n%s of %s/%s%s:\n%s\n```\n", label, pod.Name, container, note, text)
}

// --- small helpers ---

// allContainerStatuses returns the pod's init and regular container statuses
// as one slice, so health checks never overlook a failing init container.
func allContainerStatuses(p *corev1.Pod) []corev1.ContainerStatus {
	out := make([]corev1.ContainerStatus, 0, len(p.Status.InitContainerStatuses)+len(p.Status.ContainerStatuses))
	out = append(out, p.Status.InitContainerStatuses...)
	out = append(out, p.Status.ContainerStatuses...)
	return out
}

func age(t time.Time) string { return duration.HumanDuration(time.Since(t)) }

func ptrInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func ptrBool(p *bool) bool { return p != nil && *p }

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
