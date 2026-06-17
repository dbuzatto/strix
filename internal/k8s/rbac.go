package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	authnv1 "k8s.io/api/authentication/v1"
	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"
)

// subjectRef is an RBAC subject (the "who" of a permission).
type subjectRef struct {
	kind      string // "User", "Group", "ServiceAccount"
	name      string
	namespace string // only for ServiceAccount
}

func (s subjectRef) String() string {
	if s.kind == "ServiceAccount" {
		return fmt.Sprintf("ServiceAccount %s/%s", s.namespace, s.name)
	}
	return fmt.Sprintf("%s %s", s.kind, s.name)
}

// CanIRequest asks whether a subject may perform an action. When AsUser is
// empty the check runs as the current kubeconfig identity.
type CanIRequest struct {
	Verb      string
	Resource  string // raw, e.g. "pods", "deploy", "deployments.apps"
	Name      string
	Namespace string
	AsUser    string
	AsGroups  []string
}

// CanIResult is the outcome of an access review plus, when denied, a concrete
// fix: the RBAC that would grant the action.
type CanIResult struct {
	Allowed       bool
	Reason        string
	Resolved      string // resolved "resource.group"
	Namespaced    bool
	Subject       string
	CurrentGrants []string
	Suggestion    string // ready-to-apply YAML, set when denied
}

// CanI answers whether the (current or impersonated) subject may perform an
// action, and — unlike `kubectl auth can-i` — when the answer is no it explains
// the gap and emits the exact Role/RoleBinding that closes it. RBAC "Forbidden"
// errors are among the most common and most confusing in Kubernetes.
func (c *Client) CanI(ctx context.Context, req CanIRequest) (CanIResult, error) {
	mapper, err := c.restMapper()
	if err != nil {
		return CanIResult{}, fmt.Errorf("building REST mapper: %w", err)
	}
	gvr, err := mapper.ResourceFor(schema.GroupVersionResource{Resource: req.Resource})
	if err != nil {
		return CanIResult{}, fmt.Errorf("unknown resource %q: %w", req.Resource, err)
	}
	namespaced := true
	if gvk, kerr := mapper.KindFor(gvr); kerr == nil {
		if m, merr := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); merr == nil {
			namespaced = m.Scope.Name() == meta.RESTScopeNameNamespace
		}
	}

	ns := req.Namespace
	if !namespaced {
		ns = "" // cluster-scoped resources ignore the namespace
	}
	attrs := &authv1.ResourceAttributes{
		Namespace: ns,
		Verb:      req.Verb,
		Group:     gvr.Group,
		Resource:  gvr.Resource,
		Name:      req.Name,
	}

	res := CanIResult{
		Resolved:   resolvedName(gvr),
		Namespaced: namespaced,
	}

	if req.AsUser != "" {
		sar := &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{
			ResourceAttributes: attrs,
			User:               req.AsUser,
			Groups:             req.AsGroups,
		}}
		out, err := c.Clientset.AuthorizationV1().SubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			return CanIResult{}, fmt.Errorf("subject access review: %w", err)
		}
		res.Allowed, res.Reason = out.Status.Allowed, out.Status.Reason
	} else {
		ssar := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attrs}}
		out, err := c.Clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, metav1.CreateOptions{})
		if err != nil {
			return CanIResult{}, fmt.Errorf("self subject access review: %w", err)
		}
		res.Allowed, res.Reason = out.Status.Allowed, out.Status.Reason
	}

	subj := c.resolveSubject(ctx, req)
	res.Subject = subj.String()

	if !res.Allowed {
		res.CurrentGrants = c.grantsForSubject(ctx, subj, ns)
		res.Suggestion = rbacSuggestion(namespaced, ns, req.Verb, gvr, subj)
	}
	return res, nil
}

// restMapper builds a discovery-backed REST mapper, wrapped in the same
// shortcut expander kubectl uses, so resource shorthands ("deploy", "po") and
// API groups resolve exactly as they do for kubectl.
func (c *Client) restMapper() (meta.RESTMapper, error) {
	groupResources, err := restmapper.GetAPIGroupResources(c.Clientset.Discovery())
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	return restmapper.NewShortcutExpander(mapper, c.Clientset.Discovery(), func(string) {}), nil
}

// resolveSubject determines the subject the review ran as. For an impersonated
// user it parses --as; for the current identity it asks the API who we are.
func (c *Client) resolveSubject(ctx context.Context, req CanIRequest) subjectRef {
	if req.AsUser != "" {
		return parseSubject(req.AsUser)
	}
	// SelfSubjectReview (k8s 1.27+) reports the authenticated identity.
	if ssr, err := c.Clientset.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authnv1.SelfSubjectReview{}, metav1.CreateOptions{}); err == nil {
		if u := ssr.Status.UserInfo.Username; u != "" {
			return parseSubject(u)
		}
	}
	return subjectRef{kind: "User", name: "<your-user>"}
}

// parseSubject turns an identity string into an RBAC subject. The
// "system:serviceaccount:<ns>:<name>" form becomes a ServiceAccount; anything
// else is treated as a User.
func parseSubject(s string) subjectRef {
	const saPrefix = "system:serviceaccount:"
	if strings.HasPrefix(s, saPrefix) {
		rest := strings.TrimPrefix(s, saPrefix)
		if i := strings.IndexByte(rest, ':'); i > 0 {
			return subjectRef{kind: "ServiceAccount", namespace: rest[:i], name: rest[i+1:]}
		}
	}
	return subjectRef{kind: "User", name: s}
}

// grantsForSubject lists, best-effort, the roles already bound to the subject
// (namespaced RoleBindings in ns plus ClusterRoleBindings) so the gap is clear.
func (c *Client) grantsForSubject(ctx context.Context, subj subjectRef, ns string) []string {
	var grants []string
	if ns != "" {
		if rbs, err := c.Clientset.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{}); err == nil {
			for i := range rbs.Items {
				rb := &rbs.Items[i]
				if bindingHasSubject(rb.Subjects, subj) {
					grants = append(grants, fmt.Sprintf("RoleBinding %s/%s → %s/%s", rb.Namespace, rb.Name, rb.RoleRef.Kind, rb.RoleRef.Name))
				}
			}
		}
	}
	if crbs, err := c.Clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range crbs.Items {
			crb := &crbs.Items[i]
			if bindingHasSubject(crb.Subjects, subj) {
				grants = append(grants, fmt.Sprintf("ClusterRoleBinding %s → %s/%s", crb.Name, crb.RoleRef.Kind, crb.RoleRef.Name))
			}
		}
	}
	sort.Strings(grants)
	return grants
}

// bindingHasSubject reports whether any of a binding's subjects is subj.
func bindingHasSubject(subjects []rbacv1.Subject, subj subjectRef) bool {
	for _, s := range subjects {
		if s.Kind != subj.kind || s.Name != subj.name {
			continue
		}
		if subj.kind == "ServiceAccount" && s.Namespace != subj.namespace {
			continue
		}
		return true
	}
	return false
}

// rbacSuggestion renders the Role(+Binding) or ClusterRole(+Binding) that would
// grant verb on the resource to the subject. It is pure so the rendering can be
// unit-tested.
func rbacSuggestion(namespaced bool, ns, verb string, gvr schema.GroupVersionResource, subj subjectRef) string {
	apiGroup := gvr.Group // "" for core resources
	roleKind, bindKind := "ClusterRole", "ClusterRoleBinding"
	nsLine := ""
	if namespaced {
		roleKind, bindKind = "Role", "RoleBinding"
		nsLine = "\n  namespace: " + ns
	}
	name := fmt.Sprintf("strix-%s-%s", verb, gvr.Resource)

	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: rbac.authorization.k8s.io/v1\nkind: %s\nmetadata:\n  name: %s%s\nrules:\n", roleKind, name, nsLine)
	fmt.Fprintf(&b, "  - apiGroups: [%q]\n    resources: [%q]\n    verbs: [%q]\n", apiGroup, gvr.Resource, verb)
	fmt.Fprintf(&b, "---\napiVersion: rbac.authorization.k8s.io/v1\nkind: %s\nmetadata:\n  name: %s%s\nsubjects:\n", bindKind, name, nsLine)
	fmt.Fprintf(&b, "  - kind: %s\n    name: %s\n", subj.kind, subj.name)
	switch subj.kind {
	case "ServiceAccount":
		fmt.Fprintf(&b, "    namespace: %s\n", subj.namespace)
	default:
		b.WriteString("    apiGroup: rbac.authorization.k8s.io\n")
	}
	fmt.Fprintf(&b, "roleRef:\n  kind: %s\n  name: %s\n  apiGroup: rbac.authorization.k8s.io\n", roleKind, name)
	return b.String()
}

// resolvedName formats a GVR as "resource.group" (or just "resource" for core).
func resolvedName(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return gvr.Resource
	}
	return gvr.Resource + "." + gvr.Group
}
