package k8s

import (
	"strings"
	"testing"
)

func TestRolloutFindings(t *testing.T) {
	t.Run("image bump in latest rollout", func(t *testing.T) {
		revs := []revSummary{
			{revision: 3, images: []string{"api=repo/api:v2"}, replicas: 3},
			{revision: 2, images: []string{"api=repo/api:v1"}, replicas: 0},
		}
		got := strings.Join(rolloutFindings(revs), "\n")
		if !strings.Contains(got, "repo/api:v1 → repo/api:v2") {
			t.Fatalf("expected image diff, got: %q", got)
		}
		if !strings.Contains(got, "revision 2") {
			t.Fatalf("expected undo target revision 2, got: %q", got)
		}
	})

	t.Run("no change when images equal", func(t *testing.T) {
		revs := []revSummary{
			{revision: 2, images: []string{"api=repo/api:v1"}, replicas: 3},
			{revision: 1, images: []string{"api=repo/api:v1"}, replicas: 0},
		}
		if got := rolloutFindings(revs); len(got) != 0 {
			t.Fatalf("expected no findings, got: %v", got)
		}
	})

	t.Run("stuck rollout with two active revisions", func(t *testing.T) {
		revs := []revSummary{
			{revision: 5, images: []string{"api=repo/api:v2"}, replicas: 1},
			{revision: 4, images: []string{"api=repo/api:v2"}, replicas: 3},
		}
		got := strings.Join(rolloutFindings(revs), "\n")
		if !strings.Contains(got, "Revisions 5, 4 all have running pods") {
			t.Fatalf("expected stuck-rollout finding, got: %q", got)
		}
	})

	t.Run("single revision yields nothing", func(t *testing.T) {
		revs := []revSummary{{revision: 1, images: []string{"api=repo/api:v1"}, replicas: 3}}
		if got := rolloutFindings(revs); len(got) != 0 {
			t.Fatalf("expected no findings for single revision, got: %v", got)
		}
	})
}
