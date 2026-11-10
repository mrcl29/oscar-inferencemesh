# Proposed lifecycle and controller work

TODO(FG-05, FG-07): Application owns lifecycle-aware reconciliation, not the HTTP
verification endpoint. A future `reconciliation_controller.go` and
`worker_lifecycle.go` are provisional filenames within this package, not new layers.

Expected behavior: reconcile on a bounded interval independently of requests;
cancel on shutdown; distinguish absent, provisioning, model-loading, ready and
failed capacity. Coalesce concurrent cold-start demand, bound readiness waits,
surface timeouts, and avoid repeated provisioning while capacity is pending.
Define request behavior while no worker is ready explicitly before implementation;
do not silently invent a ready endpoint or unbounded request queue.

Interfaces: evolve existing discovery and OSCAR ports with lifecycle observations
and explicit errors; keep policy evaluation separate from control-plane effects.
Dependencies: honest endpoint discovery, valid/fresh metrics, configured policy
thresholds and bounds, idempotent OSCAR actions, and a server-owned cancellation
context. Do not make verification reads trigger mutations.

Acceptance tests: fake clock drives reconciliation without HTTP traffic; shutdown
cancels work; concurrent demand produces one pending action; readiness gates
routing; failed/timed-out provisioning is observable; retries are bounded;
cooldown prevents oscillation; pass-through/no-op behavior remains testable.
