package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// TriageReport is the assembled evidence plus a count of distinct issues found
// and a structured list of findings for machine-readable (--json) output.
type TriageReport struct {
	Evidence string
	Issues   int
	Scope    string
	Findings []TriageFinding
}

// TriageFinding is a single structured problem, suitable for JSON output and
// CI/cron health gates.
type TriageFinding struct {
	Category  string `json:"category"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	Detail    string `json:"detail"`
}

// maxPerCategory caps how many entries of each kind go into the evidence, so a
// badly broken namespace still produces a compact, affordable prompt.
const maxPerCategory = 40

// Triage scans a namespace (or all namespaces when allNS) for unhealthy
// workloads and recent warnings, assembling a compact evidence bundle. It does
// not fetch logs, keeping the sweep cheap enough for a single consolidated AI
// analysis. Issues reports how many problems were found across all categories.
func (c *Client) Triage(ctx context.Context, ns string, allNS bool) (TriageReport, error) {
	scope := ns
	scopeLabel := ns
	if allNS {
		scope = ""
		scopeLabel = "all namespaces"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Strix triage: %s (context=%s)\n\n", scopeLabel, c.Context)
	issues := 0
	var findings []TriageFinding

	// Unhealthy pods — the most direct signal.
	pods, err := c.Clientset.CoreV1().Pods(scope).List(ctx, metav1.ListOptions{})
	if err != nil {
		return TriageReport{}, fmt.Errorf("listing pods: %w", err)
	}
	var lines []string
	for i := range pods.Items {
		if issue := podIssue(&pods.Items[i]); issue != "" {
			lines = append(lines, issue)
		}
	}
	issues += writeCategory(&b, &findings, "Unhealthy pods", lines)

	// Deployments not at their desired replica count.
	deps, err := c.Clientset.AppsV1().Deployments(scope).List(ctx, metav1.ListOptions{})
	if err == nil {
		lines = lines[:0]
		for i := range deps.Items {
			d := &deps.Items[i]
			desired := ptrInt32(d.Spec.Replicas)
			if d.Status.ReadyReplicas < desired || d.Status.UnavailableReplicas > 0 {
				line := fmt.Sprintf("%s/%s desired=%d ready=%d available=%d", d.Namespace, d.Name, desired, d.Status.ReadyReplicas, d.Status.AvailableReplicas)
				line += falseConditions(d.Status.Conditions)
				lines = append(lines, line)
			}
		}
		issues += writeCategory(&b, &findings, "Deployments below desired replicas", lines)
	}

	// StatefulSets not fully ready.
	if stss, serr := c.Clientset.AppsV1().StatefulSets(scope).List(ctx, metav1.ListOptions{}); serr == nil {
		lines = lines[:0]
		for i := range stss.Items {
			s := &stss.Items[i]
			desired := ptrInt32(s.Spec.Replicas)
			if s.Status.ReadyReplicas < desired {
				lines = append(lines, fmt.Sprintf("%s/%s desired=%d ready=%d", s.Namespace, s.Name, desired, s.Status.ReadyReplicas))
			}
		}
		issues += writeCategory(&b, &findings, "StatefulSets below desired replicas", lines)
	}

	// DaemonSets with unavailable or unready pods.
	if dss, derr := c.Clientset.AppsV1().DaemonSets(scope).List(ctx, metav1.ListOptions{}); derr == nil {
		lines = lines[:0]
		for i := range dss.Items {
			d := &dss.Items[i]
			if d.Status.NumberUnavailable > 0 || d.Status.NumberReady < d.Status.DesiredNumberScheduled {
				lines = append(lines, fmt.Sprintf("%s/%s desired=%d ready=%d unavailable=%d", d.Namespace, d.Name, d.Status.DesiredNumberScheduled, d.Status.NumberReady, d.Status.NumberUnavailable))
			}
		}
		issues += writeCategory(&b, &findings, "DaemonSets with unavailable pods", lines)
	}

	// Jobs with failures.
	if jobs, jerr := c.Clientset.BatchV1().Jobs(scope).List(ctx, metav1.ListOptions{}); jerr == nil {
		lines = lines[:0]
		for i := range jobs.Items {
			j := &jobs.Items[i]
			if j.Status.Failed > 0 {
				line := fmt.Sprintf("%s/%s failed=%d succeeded=%d", j.Namespace, j.Name, j.Status.Failed, j.Status.Succeeded)
				for _, cond := range j.Status.Conditions {
					if cond.Type == "Failed" && cond.Status == corev1.ConditionTrue {
						line += fmt.Sprintf(" | Failed: %s %s", cond.Reason, strings.TrimSpace(cond.Message))
					}
				}
				lines = append(lines, line)
			}
		}
		issues += writeCategory(&b, &findings, "Jobs with failures", lines)
	}

	// Recent warning events tie the symptoms together (scheduling, image pulls…).
	events, eerr := c.Clientset.CoreV1().Events(scope).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("type", "Warning").String(),
	})
	if eerr == nil && len(events.Items) > 0 {
		sort.Slice(events.Items, func(i, j int) bool {
			return events.Items[i].LastTimestamp.After(events.Items[j].LastTimestamp.Time)
		})
		lines = lines[:0]
		for i := range events.Items {
			e := &events.Items[i]
			obj := e.InvolvedObject.Name
			if e.InvolvedObject.Namespace != "" {
				obj = e.InvolvedObject.Namespace + "/" + obj
			}
			lines = append(lines, fmt.Sprintf("[%s] %s x%d: %s", obj, e.Reason, e.Count, strings.TrimSpace(e.Message)))
		}
		// Warnings are informational context, not counted as distinct issues.
		writeCategory(&b, &findings, "Recent warning events", lines)
	}

	return TriageReport{Evidence: b.String(), Issues: issues, Scope: scopeLabel, Findings: findings}, nil
}

// podIssue returns a one-line description of why a pod is unhealthy, or "" when
// it looks fine. Transient states (ContainerCreating, PodInitializing) are
// ignored; stuck-but-Pending pods are surfaced via warning events instead.
// Init containers are checked too: a pod stuck in Init:CrashLoopBackOff never
// reaches its main containers, so they would otherwise look innocently empty.
func podIssue(p *corev1.Pod) string {
	var problems []string
	for _, cs := range allContainerStatuses(p) {
		if w := cs.State.Waiting; w != nil && w.Reason != "" && w.Reason != "ContainerCreating" && w.Reason != "PodInitializing" {
			problems = append(problems, fmt.Sprintf("%s Waiting(%s)", cs.Name, w.Reason))
		}
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			problems = append(problems, fmt.Sprintf("%s Terminated(%s exit=%d)", cs.Name, t.Reason, t.ExitCode))
		}
		if cs.RestartCount >= 5 {
			problems = append(problems, fmt.Sprintf("%s restarts=%d", cs.Name, cs.RestartCount))
		}
	}
	phaseBad := p.Status.Phase == corev1.PodFailed || p.Status.Phase == corev1.PodUnknown
	if len(problems) == 0 && !phaseBad {
		return ""
	}
	out := fmt.Sprintf("%s/%s phase=%s", p.Namespace, p.Name, p.Status.Phase)
	if p.Status.Reason != "" {
		out += " reason=" + p.Status.Reason
	}
	if len(problems) > 0 {
		out += " | " + strings.Join(problems, ", ")
	}
	return out
}

// writeCategory appends a "## Title (n)" section listing up to maxPerCategory
// entries to the evidence, mirrors those entries into findings as structured
// records, and returns how many entries it found (the full count, not capped).
func writeCategory(b *strings.Builder, findings *[]TriageFinding, title string, lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	fmt.Fprintf(b, "## %s (%d)\n", title, len(lines))
	shown := lines
	if len(shown) > maxPerCategory {
		shown = shown[:maxPerCategory]
	}
	for _, l := range shown {
		fmt.Fprintf(b, "- %s\n", l)
		ns, name := splitNsName(l)
		*findings = append(*findings, TriageFinding{Category: title, Namespace: ns, Name: name, Detail: l})
	}
	if len(lines) > len(shown) {
		fmt.Fprintf(b, "- …and %d more\n", len(lines)-len(shown))
	}
	b.WriteString("\n")
	return len(lines)
}

// splitNsName best-effort extracts a "namespace/name" prefix from a finding
// line for structured output. Lines look like "prod/api phase=..." or
// "[prod/api] Reason x3: ...". Returns empty strings when no ns/name is found.
func splitNsName(line string) (ns, name string) {
	tok := line
	if i := strings.IndexAny(tok, " \t"); i >= 0 {
		tok = tok[:i]
	}
	tok = strings.Trim(tok, "[]")
	slash := strings.IndexByte(tok, '/')
	if slash <= 0 || slash == len(tok)-1 {
		return "", ""
	}
	return tok[:slash], tok[slash+1:]
}

// falseConditions formats any deployment conditions that are currently False,
// which usually carry the reason the workload is unhealthy.
func falseConditions(conds []appsv1.DeploymentCondition) string {
	var out string
	for _, c := range conds {
		if c.Status == corev1.ConditionFalse {
			out += fmt.Sprintf(" | %s=False: %s", c.Type, c.Reason)
		}
	}
	return out
}
