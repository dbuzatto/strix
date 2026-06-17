package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// writeService gathers a Service's spec, its backing pods, endpoints and any
// NetworkPolicies that select those pods, then leads with a deterministic
// diagnosis of the most common Service failures (a selector that matches no
// pods, zero ready endpoints, a LoadBalancer with no address). Networking is
// the single largest source of cluster incidents, so the goal is to point at
// the break before the AI even runs.
func (c *Client) writeService(ctx context.Context, b *strings.Builder, svc *corev1.Service) {
	fmt.Fprintf(b, "## Service %s\n", svc.Name)
	fmt.Fprintf(b, "- Type: %s | ClusterIP: %s\n", svc.Spec.Type, svc.Spec.ClusterIP)
	if len(svc.Spec.Selector) > 0 {
		fmt.Fprintf(b, "- Selector: %s\n", labels.Set(svc.Spec.Selector).String())
	} else {
		fmt.Fprintf(b, "- Selector: <none> (endpoints managed manually)\n")
	}
	if ports := servicePorts(svc); ports != "" {
		fmt.Fprintf(b, "- Ports: %s\n", ports)
	}
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		fmt.Fprintf(b, "- ExternalName: %s\n", svc.Spec.ExternalName)
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		addr := ing.IP
		if addr == "" {
			addr = ing.Hostname
		}
		fmt.Fprintf(b, "- LoadBalancer address: %s\n", addr)
	}
	fmt.Fprintf(b, "- Age: %s\n\n", age(svc.CreationTimestamp.Time))

	// Resolve the pods the selector points at and how many are Ready — the
	// Service routes only to Ready pods, so this is where most "it returns 503"
	// problems live.
	var matched, ready int
	var podLabels []labels.Set
	if len(svc.Spec.Selector) > 0 {
		sel := labels.SelectorFromSet(svc.Spec.Selector)
		if pods, err := c.Clientset.CoreV1().Pods(svc.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel.String()}); err == nil {
			matched = len(pods.Items)
			for i := range pods.Items {
				podLabels = append(podLabels, labels.Set(pods.Items[i].Labels))
				if podReady(&pods.Items[i]) {
					ready++
				}
			}
			fmt.Fprintf(b, "Backing pods: %d match the selector, %d Ready.\n", matched, ready)
			for i := range pods.Items {
				p := &pods.Items[i]
				if !podReady(p) {
					fmt.Fprintf(b, "  - not ready: %s phase=%s\n", p.Name, p.Status.Phase)
				}
			}
			b.WriteString("\n")
		}
	}

	// Endpoints carry the authoritative ready/not-ready address counts the
	// kube-proxy actually programs.
	readyEPs, notReadyEPs := -1, 0
	if eps, err := c.Clientset.CoreV1().Endpoints(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{}); err == nil {
		readyEPs, notReadyEPs = 0, 0
		for _, sub := range eps.Subsets {
			readyEPs += len(sub.Addresses)
			notReadyEPs += len(sub.NotReadyAddresses)
		}
		fmt.Fprintf(b, "Endpoints: %d ready, %d not ready.\n\n", readyEPs, notReadyEPs)
	}

	// NetworkPolicies that select the backing pods can silently drop traffic.
	policies := c.policiesForPods(ctx, svc.Namespace, podLabels)
	if len(policies) > 0 {
		fmt.Fprintf(b, "NetworkPolicies selecting these pods: %s\n\n", strings.Join(policies, ", "))
	}

	c.writeEvents(ctx, b, svc.Namespace, svc.Name)

	if findings := serviceFindings(svc, matched, ready, readyEPs, notReadyEPs, policies); len(findings) > 0 {
		b.WriteString("## Findings\n")
		for _, f := range findings {
			fmt.Fprintf(b, "- %s\n", f)
		}
		b.WriteString("\n")
	}
}

// serviceFindings returns a deterministic list of the most likely Service
// problems. readyEPs is -1 when the Endpoints object was absent. It is kept
// pure (no client) so the decision tree can be unit-tested.
func serviceFindings(svc *corev1.Service, matched, ready, readyEPs, notReadyEPs int, policies []string) []string {
	var out []string
	hasSelector := len(svc.Spec.Selector) > 0
	external := svc.Spec.Type == corev1.ServiceTypeExternalName

	switch {
	case external:
		// ExternalName services have no endpoints by design; nothing to flag.
	case hasSelector && matched == 0:
		out = append(out, "Selector matches 0 pods — the Service black-holes traffic. Compare its selector with the labels on the pod template (a common typo: app vs app.kubernetes.io/name).")
	case hasSelector && matched > 0 && ready == 0:
		out = append(out, fmt.Sprintf("%d pods match but none are Ready, so the Service has no endpoints to route to — check the pods' readiness probes and why they are failing.", matched))
	case !hasSelector && readyEPs <= 0:
		out = append(out, "No selector and no ready endpoints — this Service relies on a manually-managed Endpoints/EndpointSlice object that is missing or empty.")
	}

	if readyEPs == 0 && hasSelector && !external && !(matched == 0) && !(matched > 0 && ready == 0) {
		// 0 ready endpoints not already explained by the cases above.
		out = append(out, "0 ready endpoints — kube-proxy has nothing to forward to; traffic to this Service will fail.")
	}
	if readyEPs > 0 && notReadyEPs > 0 {
		out = append(out, fmt.Sprintf("%d endpoint(s) not ready alongside %d ready — the Service is running at reduced capacity.", notReadyEPs, readyEPs))
	}

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) == 0 {
		out = append(out, "LoadBalancer has no external address yet — the cloud provider hasn't provisioned one, or no load-balancer controller is installed (common on bare-metal/kind without MetalLB).")
	}
	if len(policies) > 0 {
		out = append(out, fmt.Sprintf("Backends are selected by NetworkPolicy(ies) %s — if traffic still fails with healthy endpoints, verify an ingress rule allows the client; a default-deny with no matching rule blocks it.", strings.Join(policies, ", ")))
	}
	return out
}

// writeIngress gathers an Ingress and checks each backend Service exists and has
// endpoints, plus whether referenced TLS secrets are present — the usual reasons
// an Ingress returns 503/404.
func (c *Client) writeIngress(ctx context.Context, b *strings.Builder, ing *networkingv1.Ingress) {
	fmt.Fprintf(b, "## Ingress %s\n", ing.Name)
	if ing.Spec.IngressClassName != nil {
		fmt.Fprintf(b, "- IngressClass: %s\n", *ing.Spec.IngressClassName)
	}
	for _, lb := range ing.Status.LoadBalancer.Ingress {
		addr := lb.IP
		if addr == "" {
			addr = lb.Hostname
		}
		fmt.Fprintf(b, "- Address: %s\n", addr)
	}
	fmt.Fprintf(b, "- Age: %s\n\n", age(ing.CreationTimestamp.Time))

	var findings []string
	if len(ing.Status.LoadBalancer.Ingress) == 0 {
		findings = append(findings, "Ingress has no address yet — the ingress controller may not have picked it up (check the IngressClass and that a controller is running).")
	}

	// Walk every backend Service referenced by the rules and verify it resolves.
	seen := map[string]bool{}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			svc := path.Backend.Service
			if svc == nil || seen[svc.Name] {
				continue
			}
			seen[svc.Name] = true
			host := rule.Host
			if host == "" {
				host = "*"
			}
			fmt.Fprintf(b, "Backend: host %s path %s → service %s:%s\n", host, path.Path, svc.Name, servicePortRef(svc.Port))
			if f := c.backendFinding(ctx, ing.Namespace, svc.Name); f != "" {
				findings = append(findings, f)
			}
		}
	}
	b.WriteString("\n")

	// TLS secrets must exist or the controller can't terminate HTTPS.
	for _, tls := range ing.Spec.TLS {
		if tls.SecretName == "" {
			continue
		}
		if _, err := c.Clientset.CoreV1().Secrets(ing.Namespace).Get(ctx, tls.SecretName, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			findings = append(findings, fmt.Sprintf("TLS secret %q is missing — HTTPS for %s will fail.", tls.SecretName, strings.Join(tls.Hosts, ", ")))
		}
	}

	c.writeEvents(ctx, b, ing.Namespace, ing.Name)

	if len(findings) > 0 {
		b.WriteString("## Findings\n")
		for _, f := range findings {
			fmt.Fprintf(b, "- %s\n", f)
		}
		b.WriteString("\n")
	}
}

// backendFinding returns a problem string if an Ingress backend Service is
// missing or has no ready endpoints, else "". Only a NotFound is treated as
// missing; other errors (e.g. Forbidden) mean we can't verify and stay silent.
func (c *Client) backendFinding(ctx context.Context, ns, name string) string {
	if _, err := c.Clientset.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Sprintf("Backend service %q does not exist — this route returns 503.", name)
		}
		return ""
	}
	eps, err := c.Clientset.CoreV1().Endpoints(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Sprintf("Backend service %q has no Endpoints object — this route returns 503.", name)
		}
		return ""
	}
	ready := 0
	for _, sub := range eps.Subsets {
		ready += len(sub.Addresses)
	}
	if ready == 0 {
		return fmt.Sprintf("Backend service %q has 0 ready endpoints — this route returns 503.", name)
	}
	return ""
}

// policiesForPods returns the names of NetworkPolicies in ns whose pod selector
// matches at least one of the given pod label sets. An empty policy selector
// matches every pod in the namespace.
func (c *Client) policiesForPods(ctx context.Context, ns string, podLabels []labels.Set) []string {
	if len(podLabels) == 0 {
		return nil
	}
	nps, err := c.Clientset.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var names []string
	for i := range nps.Items {
		np := &nps.Items[i]
		sel, err := metav1.LabelSelectorAsSelector(&np.Spec.PodSelector)
		if err != nil {
			continue
		}
		for _, pl := range podLabels {
			if sel.Matches(pl) {
				names = append(names, np.Name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// podReady reports whether the pod's Ready condition is True — the gate the
// Service uses to include a pod in its endpoints.
func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func servicePorts(svc *corev1.Service) string {
	parts := make([]string, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		s := fmt.Sprintf("%d/%s", p.Port, p.Protocol)
		if p.Name != "" {
			s = p.Name + ":" + s
		}
		if p.TargetPort.String() != "0" && p.TargetPort.String() != "" {
			s += "→" + p.TargetPort.String()
		}
		if p.NodePort != 0 {
			s += fmt.Sprintf(" (nodePort %d)", p.NodePort)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func servicePortRef(p networkingv1.ServiceBackendPort) string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("%d", p.Number)
}
