package k8s

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// revSummary is one Deployment revision derived from a ReplicaSet: its revision
// number, the container images it ran, and how many pods it currently has.
type revSummary struct {
	revision int
	images   []string // "container=image"
	replicas int32    // currently running pods for this revision
	age      string
}

// writeRollout reconstructs a Deployment's revision history from its
// ReplicaSets and leads with what changed in the latest rollout. Since ~85% of
// incidents trace back to a change, pinning a regression to a specific revision
// (and its image bump) is often the whole diagnosis.
func (c *Client) writeRollout(ctx context.Context, b *strings.Builder, dep *appsv1.Deployment) {
	rss, err := c.Clientset.AppsV1().ReplicaSets(dep.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	var revs []revSummary
	for i := range rss.Items {
		rs := &rss.Items[i]
		owned := false
		for _, ref := range rs.OwnerReferences {
			if ref.UID == dep.UID {
				owned = true
				break
			}
		}
		if !owned {
			continue
		}
		rev, _ := strconv.Atoi(rs.Annotations["deployment.kubernetes.io/revision"])
		revs = append(revs, revSummary{
			revision: rev,
			images:   containerImages(&rs.Spec.Template.Spec),
			replicas: rs.Status.Replicas,
			age:      age(rs.CreationTimestamp.Time),
		})
	}
	if len(revs) == 0 {
		return
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].revision > revs[j].revision })

	const maxRevs = 5
	shown := revs
	if len(shown) > maxRevs {
		shown = shown[:maxRevs]
	}
	b.WriteString("## Rollout history (newest first)\n")
	for _, r := range shown {
		fmt.Fprintf(b, "- revision %d: %d running pods, age %s, images: %s\n",
			r.revision, r.replicas, r.age, strings.Join(r.images, ", "))
	}
	b.WriteString("\n")

	if findings := rolloutFindings(revs); len(findings) > 0 {
		b.WriteString("Rollout findings:\n")
		for _, f := range findings {
			fmt.Fprintf(b, "  - %s\n", f)
		}
		b.WriteString("\n")
	}
}

// rolloutFindings inspects a revision history (sorted newest-first) and reports
// what likely changed: an image bump in the latest rollout, and whether an
// older revision still has running pods (a rollout in progress or stuck). It is
// pure so the diff logic can be unit-tested.
func rolloutFindings(revs []revSummary) []string {
	if len(revs) == 0 {
		return nil
	}
	var out []string

	// Image diff between the latest revision and the one before it.
	if len(revs) >= 2 {
		prev := imageMap(revs[1].images)
		for _, cur := range revs[0].images {
			name, img := splitImage(cur)
			if old, ok := prev[name]; ok && old != img {
				out = append(out, fmt.Sprintf("Latest rollout (revision %d) changed container %q image: %s → %s. If the problem started recently, this is the prime suspect — `kubectl rollout undo` to revision %d reverts it.",
					revs[0].revision, name, old, img, revs[1].revision))
			}
		}
	}

	// More than one revision with running pods means the rollout hasn't
	// converged: the new pods may be failing while the old ones hold traffic.
	var active []string
	for _, r := range revs {
		if r.replicas > 0 {
			active = append(active, strconv.Itoa(r.revision))
		}
	}
	if len(active) > 1 {
		out = append(out, fmt.Sprintf("Revisions %s all have running pods — the rollout is in progress or stuck; the newest revision's pods are likely failing their probes.", strings.Join(active, ", ")))
	}
	return out
}

func containerImages(spec *corev1.PodSpec) []string {
	out := make([]string, 0, len(spec.Containers))
	for _, ct := range spec.Containers {
		out = append(out, ct.Name+"="+ct.Image)
	}
	return out
}

func imageMap(images []string) map[string]string {
	m := make(map[string]string, len(images))
	for _, s := range images {
		name, img := splitImage(s)
		m[name] = img
	}
	return m
}

// splitImage splits a "container=image" entry. A missing "=" yields an empty
// container name and the whole string as the image.
func splitImage(s string) (name, image string) {
	if i := strings.IndexByte(s, '='); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}
