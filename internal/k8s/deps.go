package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// objRef is a referenced object name plus whether the reference is optional
// (an optional ConfigMap/Secret that is absent is not an error).
type objRef struct {
	name     string
	optional bool
}

// podRefs extracts every ConfigMap, Secret and PVC a pod references, plus its
// effective ServiceAccount. It is pure (no client) so the extraction can be
// unit-tested against a hand-built PodSpec. A missing required reference is one
// of the most common reasons a pod is stuck in CreateContainerConfigError,
// CrashLoopBackOff or Pending, yet it never shows up in the container logs.
func podRefs(pod *corev1.Pod) (configMaps, secrets []objRef, pvcs []string, sa string) {
	spec := pod.Spec
	cmSeen := map[string]bool{}
	secSeen := map[string]bool{}

	addCM := func(name string, optional *bool) {
		if name == "" || cmSeen[name] {
			return
		}
		cmSeen[name] = true
		configMaps = append(configMaps, objRef{name, ptrBool(optional)})
	}
	addSec := func(name string, optional *bool) {
		if name == "" || secSeen[name] {
			return
		}
		secSeen[name] = true
		secrets = append(secrets, objRef{name, ptrBool(optional)})
	}

	containers := make([]corev1.Container, 0, len(spec.InitContainers)+len(spec.Containers))
	containers = append(containers, spec.InitContainers...)
	containers = append(containers, spec.Containers...)
	for _, ct := range containers {
		for _, ef := range ct.EnvFrom {
			if ef.ConfigMapRef != nil {
				addCM(ef.ConfigMapRef.Name, ef.ConfigMapRef.Optional)
			}
			if ef.SecretRef != nil {
				addSec(ef.SecretRef.Name, ef.SecretRef.Optional)
			}
		}
		for _, e := range ct.Env {
			if e.ValueFrom == nil {
				continue
			}
			if r := e.ValueFrom.ConfigMapKeyRef; r != nil {
				addCM(r.Name, r.Optional)
			}
			if r := e.ValueFrom.SecretKeyRef; r != nil {
				addSec(r.Name, r.Optional)
			}
		}
	}

	pvcSeen := map[string]bool{}
	for _, v := range spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			addCM(v.ConfigMap.Name, v.ConfigMap.Optional)
		case v.Secret != nil:
			addSec(v.Secret.SecretName, v.Secret.Optional)
		case v.PersistentVolumeClaim != nil:
			if n := v.PersistentVolumeClaim.ClaimName; n != "" && !pvcSeen[n] {
				pvcSeen[n] = true
				pvcs = append(pvcs, n)
			}
		case v.Projected != nil:
			for _, src := range v.Projected.Sources {
				if src.ConfigMap != nil {
					addCM(src.ConfigMap.Name, src.ConfigMap.Optional)
				}
				if src.Secret != nil {
					addSec(src.Secret.Name, src.Secret.Optional)
				}
			}
		}
	}

	for _, ips := range spec.ImagePullSecrets {
		addSec(ips.Name, nil) // imagePullSecrets are always required when listed
	}

	sa = spec.ServiceAccountName
	if sa == "" {
		sa = "default"
	}
	return configMaps, secrets, pvcs, sa
}

// writePodDependencies checks that every object a pod references actually
// exists (and that PVCs are Bound), appending a Dependencies section with any
// problems found. Missing dependencies are reported as findings because they
// are invisible in logs.
func (c *Client) writePodDependencies(ctx context.Context, b *strings.Builder, pod *corev1.Pod) {
	configMaps, secrets, pvcs, sa := podRefs(pod)
	ns := pod.Namespace
	var findings []string

	// Only a genuine NotFound proves a dependency is missing. Any other error
	// (most importantly a Forbidden — Secrets are routinely RBAC-restricted even
	// when pods are readable) means we could not verify, and must not be
	// reported as "does not exist".
	for _, r := range configMaps {
		if _, err := c.Clientset.CoreV1().ConfigMaps(ns).Get(ctx, r.name, metav1.GetOptions{}); apierrors.IsNotFound(err) && !r.optional {
			findings = append(findings, fmt.Sprintf("ConfigMap %q is referenced but does not exist — the pod cannot start (CreateContainerConfigError).", r.name))
		}
	}
	for _, r := range secrets {
		if _, err := c.Clientset.CoreV1().Secrets(ns).Get(ctx, r.name, metav1.GetOptions{}); apierrors.IsNotFound(err) && !r.optional {
			findings = append(findings, fmt.Sprintf("Secret %q is referenced but does not exist — the pod cannot start (CreateContainerConfigError or ImagePullBackOff for pull secrets).", r.name))
		}
	}
	for _, name := range pvcs {
		pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			findings = append(findings, fmt.Sprintf("PVC %q is referenced but does not exist — the pod stays Pending (FailedScheduling).", name))
			continue
		}
		if err == nil && pvc.Status.Phase != corev1.ClaimBound {
			findings = append(findings, fmt.Sprintf("PVC %q is %s, not Bound — the pod stays Pending until a PersistentVolume satisfies it (check the StorageClass).", name, pvc.Status.Phase))
		}
	}
	if _, err := c.Clientset.CoreV1().ServiceAccounts(ns).Get(ctx, sa, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		findings = append(findings, fmt.Sprintf("ServiceAccount %q does not exist — the pod cannot be admitted (no token to mount).", sa))
	}

	if len(findings) == 0 {
		return
	}
	sort.Strings(findings)
	b.WriteString("Dependency problems:\n")
	for _, f := range findings {
		fmt.Fprintf(b, "  - %s\n", f)
	}
	b.WriteString("\n")
}
