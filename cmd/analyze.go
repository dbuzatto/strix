package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dbuzatto/strix/internal/ai"
	"github.com/dbuzatto/strix/internal/config"
	"github.com/dbuzatto/strix/internal/k8s"
	"github.com/dbuzatto/strix/internal/render"
	"github.com/spf13/cobra"
)

var (
	flagAnalyzeTail   int64
	flagAnalyzeRaw    bool
	flagAnalyzeOut    string
	flagAnalyzePrompt string
	flagAnalyzeLang   string
	flagAnalyzeModel  string
	flagAnalyzePlain  bool
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
  strix analyze pod/api-7d9f --raw                      # print the evidence, skip the AI
  strix analyze pod/api-7d9f --lang pt                  # answer in Portuguese
  strix analyze pod/api-7d9f --prompt "por que reinicia?"
  strix analyze deployment/api -m opus                 # pick the Claude model`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func init() {
	f := analyzeCmd.Flags()
	f.Int64Var(&flagAnalyzeTail, "tail", 100, "log lines to gather per container")
	f.BoolVar(&flagAnalyzeRaw, "raw", false, "print the gathered evidence without calling the AI")
	f.StringVarP(&flagAnalyzeOut, "out", "o", "", "write the result to a file instead of stdout")
	f.StringVar(&flagAnalyzePrompt, "prompt", "", "custom question for the AI (overrides the default root-cause analysis)")
	f.StringVar(&flagAnalyzeLang, "lang", "", "language for the AI answer (e.g. pt, en, \"português\")")
	f.StringVarP(&flagAnalyzeModel, "model", "m", "", "Claude model: opus, sonnet, haiku, or a full id (default: your Claude Code default)")
	f.BoolVar(&flagAnalyzePlain, "plain", false, "plain text output, no Markdown rendering or color")
	rootCmd.AddCommand(analyzeCmd)
}

// buildInstruction assembles the prompt sent to the AI: either the default
// root-cause template or the user's custom question, plus an optional language.
func buildInstruction(prompt, lang string) string {
	instruction := analyzeInstruction
	if prompt != "" {
		instruction = "You are a Kubernetes expert. Using the evidence provided on stdin, answer the following request:\n\n" + prompt
	}
	if lang != "" {
		instruction += "\n\nWrite your entire answer in this language: " + lang + "."
	}
	return instruction
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	ref, err := k8s.ParseRef(args[0])
	if err != nil {
		return err
	}

	// Resolve effective settings: an explicit flag wins, otherwise the config
	// file, otherwise the built-in default already held by the flag variable.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	model := flagAnalyzeModel
	if !cmd.Flags().Changed("model") && cfg.Model != "" {
		model = cfg.Model
	}
	lang := flagAnalyzeLang
	if !cmd.Flags().Changed("lang") && cfg.Lang != "" {
		lang = cfg.Lang
	}
	tail := flagAnalyzeTail
	if !cmd.Flags().Changed("tail") && cfg.Tail > 0 {
		tail = cfg.Tail
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
		TailLines: tail,
	})
	if err != nil {
		return err
	}

	output := evidence
	if !flagAnalyzeRaw {
		provider, err := ai.Detect(ai.Options{Model: model, APIKey: cfg.APIKey, Backend: cfg.Backend})
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: analyzing %s with %s...\n", ref, provider.Name())

		aiCtx, cancelAI := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancelAI()

		output, err = provider.Analyze(aiCtx, buildInstruction(flagAnalyzePrompt, lang), evidence)
		if err != nil {
			return err
		}
	}

	// Files keep raw Markdown so .md stays clean.
	if flagAnalyzeOut != "" {
		if err := os.WriteFile(flagAnalyzeOut, []byte(output+"\n"), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "strix: wrote %s\n", flagAnalyzeOut)
		return nil
	}

	// Render Markdown to ANSI only for the AI answer on an interactive terminal.
	// Raw evidence, pipes/redirects and --plain get the unrendered text so the
	// bytes stay clean for files and downstream tools.
	if !flagAnalyzeRaw && !flagAnalyzePlain && render.IsTTY() {
		if pretty, err := render.Markdown(output); err == nil {
			fmt.Print(pretty)
			return nil
		}
	}

	fmt.Println(output)
	return nil
}
