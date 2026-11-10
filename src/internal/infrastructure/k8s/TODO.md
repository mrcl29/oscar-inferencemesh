# Proposed multi-cluster discovery

TODO(FG-08): Extend discovery in this existing package. A possible
`multi_cluster_discovery.go` filename is provisional; do not add an alternate
adapter tree before confirming the configured cluster access contract.

Ownership: Kubernetes adapters discover workers; domain ports define cluster
identity and lifecycle observations; application decides eligibility and scaling.
Expected behavior: query explicitly selected clusters with bounded cancellation,
preserve origin identity and partial errors, and never equate transport failure
with zero replicas. A configured OSCAR endpoint alone does not provide Kubernetes
credentials or establish cross-cluster worker reachability.

Interfaces/dependencies: cluster-scoped discovery inputs/results, collision-safe
endpoint/metric keys, verified per-cluster access configuration, and routable worker
addresses. Reuse the current single-cluster adapter after readiness/membership fixes.

Acceptance tests: identical pod names in two clusters remain distinct; one failed
cluster produces a partial-error result; cancellation terminates queries; unready
workers never become routing candidates; credentials and destinations never leak
between clusters. Integration tests must verify gateway reachability explicitly.
