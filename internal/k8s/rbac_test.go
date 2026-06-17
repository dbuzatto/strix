package k8s

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseSubject(t *testing.T) {
	cases := []struct {
		in   string
		kind string
		name string
		ns   string
	}{
		{"system:serviceaccount:prod:ci", "ServiceAccount", "ci", "prod"},
		{"alice@example.com", "User", "alice@example.com", ""},
		{"system:serviceaccount:malformed", "User", "system:serviceaccount:malformed", ""},
	}
	for _, c := range cases {
		got := parseSubject(c.in)
		if got.kind != c.kind || got.name != c.name || got.namespace != c.ns {
			t.Errorf("parseSubject(%q) = %+v, want kind=%s name=%s ns=%s", c.in, got, c.kind, c.name, c.ns)
		}
	}
}

func TestRBACSuggestion(t *testing.T) {
	t.Run("namespaced role for a service account", func(t *testing.T) {
		gvr := schema.GroupVersionResource{Group: "apps", Resource: "deployments"}
		subj := subjectRef{kind: "ServiceAccount", name: "ci", namespace: "prod"}
		out := rbacSuggestion(true, "prod", "create", gvr, subj)
		for _, want := range []string{
			"kind: Role", "kind: RoleBinding", "namespace: prod",
			`apiGroups: ["apps"]`, `resources: ["deployments"]`, `verbs: ["create"]`,
			"kind: ServiceAccount", "name: ci", "name: strix-create-deployments",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("suggestion missing %q:\n%s", want, out)
			}
		}
		// ServiceAccount subjects must NOT carry an apiGroup line.
		if strings.Contains(out, "apiGroup: rbac.authorization.k8s.io\n    apiGroup") {
			t.Errorf("unexpected apiGroup on SA subject:\n%s", out)
		}
	})

	t.Run("cluster role for a cluster-scoped resource", func(t *testing.T) {
		gvr := schema.GroupVersionResource{Group: "", Resource: "nodes"}
		subj := subjectRef{kind: "User", name: "alice"}
		out := rbacSuggestion(false, "", "list", gvr, subj)
		for _, want := range []string{
			"kind: ClusterRole", "kind: ClusterRoleBinding",
			`apiGroups: [""]`, `resources: ["nodes"]`, `verbs: ["list"]`,
			"kind: User", "name: alice", "apiGroup: rbac.authorization.k8s.io",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("suggestion missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "namespace:") {
			t.Errorf("cluster-scoped suggestion should have no namespace:\n%s", out)
		}
	})
}
