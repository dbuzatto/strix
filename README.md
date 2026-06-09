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
it**. By default it uses the **Claude Code already installed on your machine**
(no extra API key to manage), and falls back to the **Anthropic API** when you
configure a key.

## Status

Early development. Current milestone: AI root-cause analysis of pods and
deployments.

## Roadmap

- [x] Auto-detect kubeconfig (`--kubeconfig` → `$KUBECONFIG` → `~/.kube/config`)
- [x] `strix analyze <kind/name>` — gather status + events + logs and explain
- [x] AI backend: local `claude` by default, or the Anthropic API via `api_key`
- [x] `strix top nodes|pods` — CPU/memory usage with gauges (metrics-server)
- [x] `strix top --watch` — live dashboard with real-time usage gauges (htop-like)
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

### AI backend

The backend is auto-detected: if the `claude` CLI is installed and logged in,
Strix uses it (no API key needed); otherwise it uses the Anthropic API with the
`api_key` from your config (or the `ANTHROPIC_API_KEY` environment variable).
Set `backend` in the config to force one — `claude-code`, `api`, or `auto`
(default). With the API backend, `model` accepts `opus`/`sonnet`/`haiku` or a
full model id.

### Resource usage

```bash
strix top nodes                    # node CPU/memory with usage gauges
strix top pods -A                  # pod usage across all namespaces
strix top pods -l app=api          # filter pods by label selector
strix top deployment/api -n prod   # usage of just a deployment's pods
strix top nodes --watch            # live htop-style dashboard (q to quit)
```

`top` reads the metrics-server (`metrics.k8s.io`); `--watch` takes over the
screen and returns you to the shell on `q`.

## Configuration

Set defaults once instead of passing flags every time. Generate the file with:

```bash
strix config init     # writes ~/.config/strix/config.yaml
strix config path     # show where it lives
```

```yaml
# ~/.config/strix/config.yaml
model: opus           # default Claude model (opus, sonnet, haiku, or a full id)
lang: pt              # default answer language
tail: 100             # log lines gathered per container
backend: auto         # auto | claude-code | api
api_key: ""           # Anthropic API key for the 'api' backend (keep private)
```

Precedence is **flag > config file > built-in default**. The path honors
`$STRIX_CONFIG` and `$XDG_CONFIG_HOME`. The file is written `0600` since it may
hold your API key; leave `api_key` empty to use `ANTHROPIC_API_KEY` instead.
