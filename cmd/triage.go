package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dbuzatto/strix/internal/ai"
	"github.com/dbuzatto/strix/internal/config"
	"github.com/dbuzatto/strix/internal/render"
	"github.com/spf13/cobra"
)

var (
	flagTriageRaw   bool
	flagTriageOut   string
	flagTriageLang  string
	flagTriageModel string
	flagTriagePlain bool
)

const triageInstruction = `You are a senior Kubernetes SRE running a triage sweep. The evidence below lists the unhealthy resources and recent warnings found in a namespace. Produce a prioritized triage report:

1. Verdict — one line: is the namespace healthy, degraded, or on fire.
2. Issues, ranked by urgency. Group related symptoms into a single item (e.g. a deployment below desired replicas because its pods are in CrashLoopBackOff is one issue, not three). For each: the most likely root cause and the immediate next action (a kubectl command or what to inspect).

Be concise and actionable. Do not restate every line of evidence.`

var triageCmd = &cobra.Command{
	Use:   "triage",
	Short: "Scan a namespace for unhealthy resources and explain what is on fire",
	Long: `Sweep a namespace (or all namespaces with -A) for unhealthy pods, workloads
and recent warnings, then produce a single prioritized triage report.

It gathers only status and events — no logs — so the sweep stays fast and cheap.
Use 'strix analyze <kind/name>' to drill into any individual resource it flags.

Examples:
  strix triage -n prod
  strix triage -A                       # every namespace
  strix triage -n prod --raw            # print the findings, skip the AI
  strix triage -n prod --lang pt`,
	Args: cobra.NoArgs,
	RunE: runTriage,
}

func init() {
	f := triageCmd.Flags()
	f.BoolVar(&flagTriageRaw, "raw", false, "print the gathered findings without calling the AI")
	f.StringVarP(&flagTriageOut, "out", "o", "", "write the result to a file instead of stdout")
	f.StringVar(&flagTriageLang, "lang", "", "language for the AI answer (e.g. pt, en, \"português\")")
	f.StringVarP(&flagTriageModel, "model", "m", "", "Claude model: opus, sonnet, haiku, or a full id (default: your Claude Code default)")
	f.BoolVar(&flagTriagePlain, "plain", false, "plain text output, no Markdown rendering or color")
	rootCmd.AddCommand(triageCmd)
}

func runTriage(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	model := flagTriageModel
	if !cmd.Flags().Changed("model") && cfg.Model != "" {
		model = cfg.Model
	}
	lang := flagTriageLang
	if !cmd.Flags().Changed("lang") && cfg.Lang != "" {
		lang = cfg.Lang
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	scanCtx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	report, err := client.Triage(scanCtx, client.Namespace, flagAllNS)
	if err != nil {
		return err
	}

	// Nothing wrong: say so plainly and skip the AI call entirely.
	if report.Issues == 0 {
		scope := client.Namespace
		if flagAllNS {
			scope = "all namespaces"
		}
		fmt.Printf("✓ %s looks healthy — no unhealthy resources found.\n", scope)
		return nil
	}

	output := report.Evidence
	if !flagTriageRaw {
		provider, err := ai.Detect(ai.Options{Model: model, APIKey: cfg.APIKey, Backend: cfg.Backend})
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: triaging %d issue(s) with %s...\n", report.Issues, provider.Name())

		aiCtx, cancelAI := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancelAI()

		output, err = provider.Analyze(aiCtx, withLang(triageInstruction, lang), report.Evidence)
		if err != nil {
			return err
		}
	}

	if flagTriageOut != "" {
		if err := os.WriteFile(flagTriageOut, []byte(output+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: wrote %s\n", flagTriageOut)
		return nil
	}

	if !flagTriageRaw && !flagTriagePlain && render.IsTTY() {
		if pretty, err := render.Markdown(output); err == nil {
			fmt.Print(pretty)
			return nil
		}
	}

	fmt.Println(output)
	return nil
}
