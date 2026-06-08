<div align="center">
  <img src="assets/logo.jpeg" alt="Strix" width="160" />
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
- [ ] `strix top nodes --watch` — live resource dashboards (htop-like)
- [ ] `strix logs <pod> --ai`
- [ ] `strix ctx` — switch context

## Build

```bash
go build -o strix .
```

## Usage

```bash
strix analyze pod/api-7d9f -n prod          # root-cause analysis of a pod
strix analyze deployment/api -n prod        # analyze a deployment + its pods
strix analyze deployment/api -o report.md   # save the report to a file
strix analyze pod/api-7d9f --raw            # print gathered evidence, skip the AI
```

The AI backend is auto-detected: if the `claude` CLI is installed and logged in,
Strix uses it (no API key needed); otherwise it looks for `ANTHROPIC_API_KEY`.
