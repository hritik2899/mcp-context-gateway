# MCP Context Gateway

> A production-oriented MCP gateway for discovering, routing, executing, and eventually governing tools across multiple downstream MCP servers.

**Status: Active development — architecture established, product build in progress.**

The goal is not to build another thin MCP server. The goal is to build a **gateway layer between MCP clients and a distributed set of MCP servers**, providing one controlled entry point for tool discovery, routing, execution, context management, resilience, observability, and policy.

## Vision

Without a gateway, an MCP client can become tightly coupled to many individual servers:

```text
                    MCP Client
                        │
             ┌──────────┼──────────┐
             ▼          ▼          ▼
          GitHub       Jira       DB
           MCP         MCP       MCP
```

The gateway changes that topology:

```text
                         MCP Client
                              │
                              ▼
                  ┌──────────────────────┐
                  │   MCP Context       │
                  │      Gateway         │
                  │                      │
                  │  Protocol Layer      │
                  │  Tool Registry       │
                  │  Route Registry      │
                  │  Execution Router    │
                  │  Context Engine      │
                  │  Policy              │
                  │  Resilience          │
                  │  Observability       │
                  └──────────┬───────────┘
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
        GitHub MCP       Jira MCP       Database MCP
```

The client sees one gateway. The gateway manages the complexity behind it.

## Product vision

The final system is intended to provide:

- **MCP protocol termination** — accept MCP requests through a stable gateway endpoint.
- **Tool discovery** — aggregate tools available locally and across downstream MCP servers.
- **Explicit routing** — map tools to local executors or specific downstream MCP servers.
- **Execution abstraction** — separate discovery from the mechanism used to execute a tool.
- **Context management** — control, enrich, filter, budget, and eventually compress context through tool workflows.
- **Policy and governance** — central authentication, authorization, tool policy, quotas, and safety controls.
- **Resilience** — timeouts, cancellation, safe retries, circuit breaking, and downstream failure isolation.
- **Observability** — structured logs, metrics, traces, request correlation, latency, and tool-level visibility.
- **Multi-server orchestration** — manage downstream MCP servers as independently addressable execution backends.
- **Production operability** — configuration, health checks, graceful shutdown, testing, and deployment-oriented design.

The architecture is intentionally built incrementally so every boundary remains understandable and independently testable.

## Current architecture

The project has crossed the basic MCP-server stage and now contains the core boundaries required for the gateway:

```text
                         MCP Client
                              │
                              ▼
                     ┌────────────────┐
                     │ HTTP Transport │
                     └───────┬────────┘
                             │
                             ▼
                     ┌────────────────┐
                     │  MCP Protocol  │
                     │                │
                     │ initialize     │
                     │ tools/list     │
                     │ tools/call     │
                     └───────┬────────┘
                             │
                             ▼
                     ┌────────────────┐
                     │     Router     │
                     └───────┬────────┘
                             │
                 ┌───────────┴───────────┐
                 ▼                       ▼
          ┌─────────────┐        ┌────────────────┐
          │ Tool Routes │        │ Server Registry│
          └──────┬──────┘        └───────┬────────┘
                 │                       │
          ┌──────┴──────┐          ┌─────┴──────┐
          ▼             ▼          ▼            ▼
       Local          Remote     MCP Client   MCP Client
       Executor       Backend      GitHub       Jira
          │             │
          ▼             ▼
       Local Tool   Downstream MCP
                       Server
```

### Core boundaries

| Component | Responsibility |
|---|---|
| `internal/mcp` | JSON-RPC/MCP protocol types and downstream MCP client contract |
| `internal/tools` | Tool definitions, registry, and local execution |
| `internal/router` | Tool routing and downstream server registration |
| `cmd/gateway` | HTTP service composition and application entrypoint |

These boundaries will evolve as context, policy, resilience, and observability become concrete subsystems.

## Request flow

A local tool call follows this shape:

```text
Client
  │ tools/call
  ▼
Gateway
  │
  ▼
MCP Protocol
  │
  ▼
Router
  │
  ▼
Route Registry
  │
  ▼
Execution Backend
  │
  ▼
Tool
  │
  ▼
MCP Result
  │
  ▼
Client
```

The intended remote flow is:

```text
Client
  │ tools/call
  ▼
Gateway
  │
  ▼
Router
  │
  ▼
Tool → Downstream Server
  │
  ▼
MCP Client
  │
  ▼
Remote MCP Server
  │
  ▼
Result
  │
  ▼
Gateway
  │
  ▼
Client
```

This separation allows new execution backends without coupling MCP transport to implementation details.

## Current capabilities

The foundation currently includes:

- Runnable Go HTTP gateway.
- `/healthz` health endpoint.
- JSON-RPC 2.0 request/response envelopes and errors.
- MCP initialization lifecycle and capability advertisement.
- `tools/list` discovery.
- `tools/call` execution.
- Concurrency-safe tool registry with deterministic discovery ordering.
- Local tool executor abstraction.
- Request-context propagation for cancellation.
- Execution router abstraction.
- Downstream MCP client abstraction.
- Downstream MCP server registry.
- Explicit local/remote tool routing model.
- Initial unit tests around routing invariants.

This describes the current implementation, not the end state.

## Design principles

### 1. Protocol is not execution

MCP transport should not know whether a tool is implemented locally, remotely, or through another backend.

```text
Protocol → Router → Execution Backend
```

### 2. Discovery is not execution

Knowing that a tool exists is different from knowing how to execute it.

```text
Registry ≠ Executor
```

### 3. Gateway owns policy

Cross-cutting concerns belong at the gateway boundary rather than being duplicated across every downstream server.

### 4. Cancellation must propagate

A cancelled client request should be able to cancel downstream work whenever the backend supports cancellation.

### 5. Prefer explicit routing

Tool ownership and backend selection should be observable and explainable.

### 6. Incremental architecture

Each layer should be introduced because a real requirement exists. Avoid abstraction for abstraction's sake.

### 7. Production behavior matters

Latency, concurrency, failure modes, memory, network I/O, observability, and operational recovery are first-class design concerns.

## Repository structure

```text
mcp-context-gateway/
├── cmd/
│   └── gateway/
│       └── main.go
│
├── internal/
│   ├── mcp/
│   │   ├── client.go
│   │   └── protocol.go
│   │
│   ├── router/
│   │   ├── router.go
│   │   ├── routes.go
│   │   ├── routes_test.go
│   │   └── servers.go
│   │
│   └── tools/
│       ├── executor.go
│       └── registry.go
│
├── go.mod
└── README.md
```

The structure will evolve as the context engine, policy, resilience, and observability subsystems become concrete.

## Running locally

Requirements:

- Go 1.22+

Start the gateway:

```bash
go run ./cmd/gateway
```

Health check:

```bash
curl http://localhost:8080/healthz
```

Expected response:

```json
{"status":"ok"}
```

Run tests:

```bash
go test ./...
```

## Engineering direction

This repository is being built as a long-running systems project rather than a one-shot demo.

The implementation will progress through measurable architectural increments:

```text
MCP Server
    ↓
MCP Gateway
    ↓
Multi-server Router
    ↓
Context-aware Gateway
    ↓
Policy + Governance
    ↓
Resilient Gateway
    ↓
Observable Platform
    ↓
Production-grade MCP Infrastructure
```

The objective is to make the final system useful not only as an MCP integration layer, but as a **control plane for AI tool access and context flow across distributed services**.

## Status

🚧 **Active development**

The architecture is established, the core protocol and routing foundations are implemented, and the product is being built incrementally toward the full gateway vision described above.
