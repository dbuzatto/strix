package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/dbuzatto/strix/internal/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/duration"
)

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "List Kubernetes resources",
}

var getPodsCmd = &cobra.Command{
	Use:     "pods",
	Aliases: []string{"pod", "po"},
	Short:   "List pods in the current namespace",
	RunE:    runGetPods,
}

func init() {
	getCmd.AddCommand(getPodsCmd)
	rootCmd.AddCommand(getCmd)
}

func runGetPods(cmd *cobra.Command, args []string) error {
	client, err := k8s.New(k8s.Options{
		Kubeconfig: flagKubeconfig,
		Context:    flagContext,
		Namespace:  flagNamespace,
	})
	if err != nil {
		return err
	}

	ns := client.Namespace
	if flagAllNS {
		ns = metav1.NamespaceAll
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	pods, err := client.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()

	if flagAllNS {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tREADY\tSTATUS\tRESTARTS\tAGE")
	} else {
		fmt.Fprintln(w, "NAME\tREADY\tSTATUS\tRESTARTS\tAGE")
	}

	for i := range pods.Items {
		p := &pods.Items[i]
		ready, total := podReady(p)
		age := duration.HumanDuration(time.Since(p.CreationTimestamp.Time))
		if flagAllNS {
			fmt.Fprintf(w, "%s\t%s\t%d/%d\t%s\t%d\t%s\n",
				p.Namespace, p.Name, ready, total, podStatus(p), podRestarts(p), age)
		} else {
			fmt.Fprintf(w, "%s\t%d/%d\t%s\t%d\t%s\n",
				p.Name, ready, total, podStatus(p), podRestarts(p), age)
		}
	}

	return nil
}

// podReady returns the number of ready containers and the total container count.
func podReady(p *corev1.Pod) (ready, total int) {
	total = len(p.Status.ContainerStatuses)
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	return ready, total
}

// podRestarts sums restart counts across all containers in the pod.
func podRestarts(p *corev1.Pod) int {
	var restarts int32
	for _, cs := range p.Status.ContainerStatuses {
		restarts += cs.RestartCount
	}
	return int(restarts)
}

// podStatus reports the human-facing status, preferring a container
// waiting/terminated reason (e.g. CrashLoopBackOff) over the raw phase.
func podStatus(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	for _, cs := range p.Status.ContainerStatuses {
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			return cs.State.Waiting.Reason
		case cs.State.Terminated != nil && cs.State.Terminated.Reason != "":
			return cs.State.Terminated.Reason
		}
	}
	return string(p.Status.Phase)
}
