# Testing and operations

## Verification commands

```bash
make check
python3 -m venv .venv
.venv/bin/pip install -r tests/interop/requirements.txt
go build -o gateway ./cmd/gateway
.venv/bin/python tests/interop/check.py --binary ./gateway
```

The compatibility check uses the official Python MCP SDK 1.26.0 on both sides of the gateway. It checks initialization, aggregated discovery, a JSON downstream, an SSE downstream, structured results, tool errors, invalid arguments, ping, and session cleanup. Go integration tests additionally exercise authentication and an unavailable downstream.

Race tests exercise concurrent discovery snapshots and local registration/execution. They do not establish production capacity. Measure representative tool sizes, downstream latency, principal counts, and concurrency in your own environment before setting an SLO.

## Operational endpoints

- `/healthz`: process liveness; 200 while HTTP is serving.
- `/readyz`: 200 only when every configured downstream passed its latest discovery refresh; otherwise 503. Discovery is cached, so detection latency includes the refresh interval and timeout.
- `/metrics`: Prometheus text metrics for HTTP and tool counts, errors, and cumulative latency. Protected by the configured bearer authentication. JSON-RPC errors often have HTTP 200; monitor tool error metrics as well as HTTP status.

Readiness is deliberately strict even though healthy routes continue working during a partial outage. If deployed behind a readiness-gated service, an unhealthy dependency can remove the entire instance from service; choose the deployment policy that matches your availability requirements.

Logs contain request IDs, authenticated principal, server/tool names, latency, and outcome. They exclude request arguments, result bodies, and bearer tokens. Ship logs to your own durable audit sink. The gateway does not provide OpenTelemetry spans or durable storage.

`SIGTERM`/`SIGINT` drains HTTP work within `shutdownTimeout`, cancels remaining work, stops refresh, and closes downstream sessions. `SIGHUP` triggers discovery refresh only; configuration and credentials are immutable until restart. The catalog removal API withdraws routes immediately and allows already admitted calls to finish; its caller owns downstream cleanup after the grace period.

## Container

```bash
docker build -t mcp-context-gateway:local .
export GATEWAY_TOKEN="$(openssl rand -hex 32)"
docker run --rm -p 127.0.0.1:8080:8080 \
  -e GATEWAY_TOKEN mcp-context-gateway:local
```

The image runs as a non-root user with a static Go binary. Its default config exposes only `health.check`. Mount a custom file at `/etc/gateway/config.json` to configure downstreams. Container-local `127.0.0.1` refers to the container itself, not the host or another container.

Build with a supported patched Go release (`--build-arg GO_VERSION=<version>`) and pin image digests in your release pipeline. CI builds the Dockerfile and runs `python3 tests/container/check.py --image mcp-context-gateway:ci`. The smoke test verifies a non-root process with a read-only filesystem, liveness/readiness, authentication, Origin rejection, MCP initialization and tool execution, and metrics. Publishing images and deploying Kubernetes remain separate operator actions.

## Kubernetes template

`deploy/kubernetes.yaml` is a single-replica template using the same authenticated container config. Build and load/push your image, create the referenced `gateway-credentials` secret with key `token`, and update the image reference before applying. Do not commit token values.

Use a TLS ingress or trusted reverse proxy. The process serves HTTP, not TLS. Keep health endpoints private where required and restrict downstream egress. Multiple replicas require session affinity; all session/rate-limit state is process-local. Do not claim tenant-specific downstream isolation: each configured downstream uses one shared service identity.

## Failure exercises

1. Stop one fixture: after refresh, readiness is degraded and its tools disappear while healthy routes remain executable.
2. Send invalid tool arguments: the gateway returns `isError: true` without calling downstream.
3. Send a denied tool call with a reader token: discovery hides it and execution returns unknown tool.
4. Return repeated transport errors from a fixture: its circuit opens and permits a single recovery probe after cooldown.
5. Cancel a slow call: the request context is cancelled and an MCP cancellation notification is attempted downstream.
6. Exceed body/output/concurrency limits: requests fail within configured bounds. Side-effecting calls are never replayed automatically after a timeout or disconnect.

This repository does not assert a throughput or latency target without a recorded workload and environment.
