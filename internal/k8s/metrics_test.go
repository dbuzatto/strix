package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeploymentSelector(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "tier",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"web", "worker"},
				}},
			},
		},
	}
	c := &Client{Clientset: fake.NewSimpleClientset(dep)}

	got, err := c.DeploymentSelector(context.Background(), "prod", "api")
	if err != nil {
		t.Fatalf("DeploymentSelector() error: %v", err)
	}
	// matchExpressions must survive, not just matchLabels.
	want := "app=api,tier in (web,worker)"
	if got != want {
		t.Fatalf("DeploymentSelector() = %q, want %q", got, want)
	}

	if _, err := c.DeploymentSelector(context.Background(), "prod", "missing"); err == nil {
		t.Fatal("DeploymentSelector(missing) returned nil error, want not-found")
	}
}
