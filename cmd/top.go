package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dbuzatto/strix/internal/k8s"
	"github.com/dbuzatto/strix/internal/tui"
	"github.com/spf13/cobra"
)

var flagTopWatch bool

var topCmd = &cobra.Command{
	Use:   "top",
	Short: "Show CPU/memory usage (requires metrics-server)",
}

var topNodesCmd = &cobra.Command{
	Use:     "nodes",
	Aliases: []string{"node", "no"},
	Short:   "Show node resource usage",
	Args:    cobra.NoArgs,
	RunE:    runTopNodes,
}

var topPodsCmd = &cobra.Command{
	Use:     "pods",
	Aliases: []string{"pod", "po"},
	Short:   "Show pod resource usage",
	Args:    cobra.NoArgs,
	RunE:    runTopPods,
}

func init() {
	topCmd.PersistentFlags().BoolVarP(&flagTopWatch, "watch", "w", false, "live dashboard with real-time graphs (htop-style)")
	topCmd.AddCommand(topNodesCmd, topPodsCmd)
	rootCmd.AddCommand(topCmd)
}

func newClient() (*k8s.Client, error) {
	return k8s.New(k8s.Options{
		Kubeconfig: flagKubeconfig,
		Context:    flagContext,
		Namespace:  flagNamespace,
	})
}

func runTopNodes(cmd *cobra.Command, args []string) error {
	client, err := newClient()
	if err != nil {
		return err
	}

	if flagTopWatch {
		return tui.Run("Nodes", client.NodeUsage)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	usage, err := client.NodeUsage(ctx)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "NODE\tCPU\tCPU%\tMEMORY\tMEM%")
	for _, u := range usage {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			u.Name,
			fmtCPU(u.CPUUsed.MilliValue()), bar(u.CPUPercent()),
			fmtMem(u.MemUsed.Value()), bar(u.MemPercent()))
	}
	return nil
}

func runTopPods(cmd *cobra.Command, args []string) error {
	client, err := newClient()
	if err != nil {
		return err
	}
	ns := client.Namespace
	if flagAllNS {
		ns = ""
	}

	if flagTopWatch {
		title := "Pods"
		if ns != "" {
			title = "Pods · " + ns
		}
		return tui.Run(title, func(ctx context.Context) ([]k8s.Usage, error) {
			return client.PodUsage(ctx, ns)
		})
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	usage, err := client.PodUsage(ctx, ns)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	if flagAllNS {
		fmt.Fprintln(w, "NAMESPACE\tPOD\tCPU\tMEMORY")
	} else {
		fmt.Fprintln(w, "POD\tCPU\tMEMORY")
	}
	for _, u := range usage {
		if flagAllNS {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", u.Namespace, u.Name, fmtCPU(u.CPUUsed.MilliValue()), fmtMem(u.MemUsed.Value()))
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\n", u.Name, fmtCPU(u.CPUUsed.MilliValue()), fmtMem(u.MemUsed.Value()))
		}
	}
	return nil
}

// bar renders a percentage as an htop-style gauge, e.g. "[####------] 42%".
// A negative percent (unknown total) shows "n/a".
func bar(pct float64) string {
	if pct < 0 {
		return "n/a"
	}
	const width = 10
	filled := int(pct/100*width + 0.5)
	if filled > width {
		filled = width
	}
	return fmt.Sprintf("[%s%s] %3.0f%%", strings.Repeat("#", filled), strings.Repeat("-", width-filled), pct)
}

// fmtCPU prints millicores the way kubectl top does, e.g. "1719m".
func fmtCPU(milli int64) string { return fmt.Sprintf("%dm", milli) }

// fmtMem prints bytes as a human-friendly Mi/Gi value.
func fmtMem(bytes int64) string {
	const mi = 1024 * 1024
	switch {
	case bytes >= 1024*mi:
		return fmt.Sprintf("%.1fGi", float64(bytes)/float64(1024*mi))
	default:
		return fmt.Sprintf("%dMi", bytes/mi)
	}
}
