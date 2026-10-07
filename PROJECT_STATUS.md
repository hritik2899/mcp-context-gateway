# Project status

The gateway now supports a complete local + multi-server HTTP tools flow, verified with independent Python MCP SDK servers and client.

## Implemented and covered

- [x] Green Go build, vet, unit tests, and race tests.
- [x] CI quality checks and independent SDK interoperability check.
- [x] Version negotiation, validated JSON-RPC envelopes, sessions, cancellation, and response limits.
- [x] Lossless downstream results and tool metadata by default.
- [x] Downstream initialization, session IDs, JSON/SSE responses, paginated discovery, and response correlation.
- [x] Strict configuration with environment-based credentials.
- [x] Atomic discovery/routing snapshots, namespacing, schema validation, periodic refresh, and failure isolation.
- [x] Graceful route withdrawal API; admitted calls may finish. Configuration changes require restart.
- [x] Authentication, principal-bound sessions, tool/server policies, Origin checks, quotas, and backpressure.
- [x] Per-server deadlines, circuit breakers, concurrency isolation, and no automatic tool-call retries.
- [x] Optional context redaction, byte budget, heuristic token budget, explicit output truncation.
- [x] Structured request/tool logs, correlation IDs, metrics, health/readiness, and graceful process shutdown.
- [x] Configuration examples, two-server fixtures, container template, and deployment guidance.

## Scope beyond this implementation

These are product extensions, not capabilities to claim as shipped:

- OAuth/OIDC integration and delegated downstream user identities.
- Persistent/distributed sessions, quotas, catalog coordination, and durable audit retention.
- Resources/prompts/sampling/elicitation/task proxying or resumable SSE streams.
- LLM summarization, model-specific tokenization, semantic/context-aware routing.
- Production load qualification, SLOs, deployment-specific TLS/secrets/egress review, and operational dashboards.

`README.md` documents the supported protocol and deployment boundaries. Add tests before extending those boundaries.
