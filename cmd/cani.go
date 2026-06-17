package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbuzatto/strix/internal/k8s"
	"github.com/spf13/cobra"
)

var (
	flagCanIAs       string
	flagCanIAsGroups []string
)

var caniCmd = &cobra.Command{
	Use:   "can-i <verb> <resource>[/name]",
	Short: "Check an RBAC permission and, if denied, show the fix",
	Long: `Answer whether you (or another subject) may perform an action — and,
unlike 'kubectl auth can-i', when the answer is no, explain the gap and print
the exact Role/RoleBinding that would grant it.

Examples:
  strix can-i create deployments -n prod
  strix can-i get pods/api-7d9f -n prod
  strix can-i list nodes                                  # cluster-scoped
  strix can-i create secrets -n prod --as system:serviceaccount:prod:ci
  strix can-i delete pods -n prod --as alice@example.com`,
	Args: cobra.ExactArgs(2),
	RunE: runCanI,
}

func init() {
	f := caniCmd.Flags()
	f.StringVar(&flagCanIAs, "as", "", "check as another subject (a username or system:serviceaccount:<ns>:<name>)")
	f.StringArrayVar(&flagCanIAsGroups, "as-group", nil, "groups to impersonate alongside --as (repeatable)")
	rootCmd.AddCommand(caniCmd)
}

func runCanI(cmd *cobra.Command, args []string) error {
	verb := args[0]
	resource, name := args[1], ""
	if i := strings.IndexByte(resource, '/'); i >= 0 {
		resource, name = resource[:i], resource[i+1:]
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	res, err := client.CanI(ctx, k8s.CanIRequest{
		Verb:      verb,
		Resource:  resource,
		Name:      name,
		Namespace: client.Namespace,
		AsUser:    flagCanIAs,
		AsGroups:  flagCanIAsGroups,
	})
	if err != nil {
		return err
	}

	scope := "cluster-wide"
	if res.Namespaced {
		scope = "in namespace " + client.Namespace
	}
	action := verb + " " + res.Resolved
	if name != "" {
		action += "/" + name
	}

	if res.Allowed {
		fmt.Printf("✓ yes — %s can %s %s.\n", res.Subject, action, scope)
		return nil
	}

	fmt.Printf("✗ no — %s cannot %s %s.\n", res.Subject, action, scope)
	if res.Reason != "" {
		fmt.Printf("  reason: %s\n", res.Reason)
	}
	if len(res.CurrentGrants) > 0 {
		fmt.Printf("\nCurrent grants for %s:\n", res.Subject)
		for _, g := range res.CurrentGrants {
			fmt.Printf("  - %s\n", g)
		}
	} else {
		fmt.Printf("\n%s has no RoleBindings in this namespace or ClusterRoleBindings.\n", res.Subject)
	}
	fmt.Printf("\nTo grant it, apply:\n\n%s\n", res.Suggestion)
	return nil
}
