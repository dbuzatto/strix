// Package cmd defines the Strix command-line interface.
package cmd

import (
	"github.com/spf13/cobra"
)

// Persistent flags shared by all commands. They mirror kubectl's global flags.
var (
	flagKubeconfig string
	flagContext    string
	flagNamespace  string
	flagAllNS      bool
)

var rootCmd = &cobra.Command{
	Use:   "strix",
	Short: "An owl that watches your Kubernetes cluster",
	Long: `Strix is a CLI for Kubernetes with AI-powered analysis.

It connects to your cluster using the same kubeconfig rules as kubectl and
explains what is going wrong: root-cause analysis of a resource (analyze),
a prioritized sweep of a namespace (triage), and live resource usage (top).
AI runs on the Claude Code already installed on your machine — no extra API
key required — or the Anthropic API when you configure a key.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagKubeconfig, "kubeconfig", "", "path to the kubeconfig file (overrides $KUBECONFIG and ~/.kube/config)")
	pf.StringVar(&flagContext, "context", "", "kubeconfig context to use (default: current-context)")
	pf.StringVarP(&flagNamespace, "namespace", "n", "", "namespace scope for the command")
	pf.BoolVarP(&flagAllNS, "all-namespaces", "A", false, "list resources across all namespaces")
}
