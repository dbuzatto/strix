<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.png" />
    <img src="assets/logo-light.png" alt="Strix" width="200" />
  </picture>
  <h1>Strix</h1>
  <p><em>An owl that watches your Kubernetes cluster.</em></p>
</div>

---

Strix is a terminal-first **analyzer** for Kubernetes. It is not a resource
browser — listing pods is what `kubectl` and `k9s` are for. Strix gathers a
resource's status, events and logs and explains **what is wrong and how to fix
it**, using the **Claude Code already installed on your machine**, so there is
no extra API key to manage.

## Status

Early development. Current milestone: AI root-cause analysis of pods and
deployments.

## Roadmap

- [x] Auto-detect kubeconfig (`--kubeconfig` → `$KUBECONFIG` → `~/.kube/config`)
- [x] `strix analyze <kind/name>` — gather status + events + logs and explain
- [x] AI backend: detect local `claude`; fall back to `ANTHROPIC_API_KEY` (API backend WIP)
- [x] `strix top nodes|pods` — CPU/memory usage with gauges (metrics-server)
- [ ] `strix top --watch` — live resource dashboards (htop-like)
- [ ] `strix logs <pod> --ai`
- [ ] `strix ctx` — switch context

## Build

```bash
go build -o strix .
```

## Usage

```bash
strix analyze pod/api-7d9f -n prod              # root-cause analysis of a pod
strix analyze deployment/api -n prod            # analyze a deployment + its pods
strix analyze deployment/api -o report.md       # save the report to a file
strix analyze pod/api-7d9f --raw                # print gathered evidence, skip the AI
strix analyze pod/api-7d9f --lang pt            # answer in Portuguese
strix analyze pod/api-7d9f --prompt "por que reinicia?"   # your own question
strix analyze deployment/api -m opus           # pick the Claude model
```

| Flag | Purpose |
| --- | --- |
| `--prompt` | Your own question, replacing the default root-cause template |
| `--lang` | Language of the answer (`pt`, `en`, `"português"`, …) |
| `-m, --model` | Claude model: `opus`, `sonnet`, `haiku`, or a full id |
| `--raw` | Print the gathered evidence without calling the AI |
| `--plain` | Plain text output, no Markdown rendering or color |
| `-o, --out` | Write the result to a file |
| `--tail` | Log lines gathered per container (default 100) |

On an interactive terminal the AI's Markdown answer is rendered to styled ANSI
(bold, colors, bullets). When output is piped or written with `-o`, raw Markdown
is emitted so files and downstream tools stay clean.

The AI backend is auto-detected: if the `claude` CLI is installed and logged in,
Strix uses it (no API key needed); otherwise it looks for `ANTHROPIC_API_KEY`.

## Configuration

Set defaults once instead of passing flags every time. Generate the file with:

```bash
strix config init     # writes ~/.config/strix/config.yaml
strix config path     # show where it lives
```

```yaml
# ~/.config/strix/config.yaml
model: opus           # default Claude model
lang: pt              # default answer language
tail: 100             # log lines gathered per container
```

Precedence is **flag > config file > built-in default**. The path honors
`$STRIX_CONFIG` and `$XDG_CONFIG_HOME`.
