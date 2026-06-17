# Demo assets

The main README references these demo assets:

- `strix-demo.svg` — animated terminal demo (triage + analyze), generated from
  real, colored strix output captured under a pty.
- `triage.svg` — a still of `strix triage -n strix-demo`.
- `analyze.svg` — a still of `strix analyze deployment/api -n strix-demo`.

All three share the same theme, palette and geometry so they render uniformly.

Generate a safe, data-free cluster to capture from with
[`examples/demo.yaml`](../../examples/demo.yaml) (fictional names, deliberately
broken workloads). Capture against a throwaway cluster (kind/minikube/k3d) — the
fixture's broken workloads exercise every diagnosis path the demo shows.
