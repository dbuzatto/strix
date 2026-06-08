package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dbuzatto/strix/internal/ai"
	"github.com/dbuzatto/strix/internal/k8s"
	"github.com/spf13/cobra"
)

var (
	flagAnalyzeTail int64
	flagAnalyzeRaw  bool
	flagAnalyzeOut  string
)

const analyzeInstruction = `You are a senior Kubernetes SRE. Analyze the evidence below (resource status, events and container logs) and produce a concise root-cause analysis.

Structure your answer as:
1. Summary — one line: healthy, or what is wrong.
2. Root cause — the single most likely cause, citing the specific evidence (an event, a log line, an exit code).
3. Fix — concrete, actionable steps (kubectl commands or YAML changes). Flag anything you are unsure about.

Be direct and do not restate the raw evidence.`

var analyzeCmd = &cobra.Command{
	Use:   "analyze <kind/name>",
	Short: "AI-powered root-cause analysis of a resource",
	Long: `Gather a resource's status, events and logs and explain what is wrong.

Supported kinds: pod, deployment.

Examples:
  strix analyze pod/api-7d9f -n prod
  strix analyze deployment/api -n prod -o report.md
  strix analyze pod/api-7d9f --raw      # print the evidence, skip the AI`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func init() {
	f := analyzeCmd.Flags()
	f.Int64Var(&flagAnalyzeTail, "tail", 100, "log lines to gather per container")
	f.BoolVar(&flagAnalyzeRaw, "raw", false, "print the gathered evidence without calling the AI")
	f.StringVarP(&flagAnalyzeOut, "out", "o", "", "write the result to a file instead of stdout")
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	ref, err := k8s.ParseRef(args[0])
	if err != nil {
		return err
	}

	client, err := k8s.New(k8s.Options{
		Kubeconfig: flagKubeconfig,
		Context:    flagContext,
		Namespace:  flagNamespace,
	})
	if err != nil {
		return err
	}

	gatherCtx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	evidence, err := client.Gather(gatherCtx, ref, k8s.GatherOptions{
		Namespace: flagNamespace,
		TailLines: flagAnalyzeTail,
	})
	if err != nil {
		return err
	}

	output := evidence
	if !flagAnalyzeRaw {
		provider, err := ai.Detect()
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: analyzing %s with %s...\n", ref, provider.Name())

		aiCtx, cancelAI := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancelAI()

		output, err = provider.Analyze(aiCtx, analyzeInstruction, evidence)
		if err != nil {
			return err
		}
	}

	if flagAnalyzeOut != "" {
		if err := os.WriteFile(flagAnalyzeOut, []byte(output+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: wrote %s\n", flagAnalyzeOut)
		return nil
	}

	fmt.Println(output)
	return nil
}
