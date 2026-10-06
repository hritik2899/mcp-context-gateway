# Configuration reference

Load with `gateway -config path.json`. Omitted fields use defaults. Unknown fields and trailing JSON are rejected. `gateway -config path.json -check` validates configuration and credential references without starting listeners or contacting servers.

| Field | Default | Meaning |
|---|---|---|
| `listen` | `127.0.0.1:8080` | Public binding requires principals |
| `requestTimeout` | `30s` | Maximum MCP request execution time |
| `discoveryTimeout` | `10s` | Timeout per downstream discovery attempt |
| `refreshInterval` | `30s` | Catalog refresh interval |
| `shutdownTimeout` | `10s` | HTTP drain and downstream cleanup budget |
| `sessionTTL` | `1h` | Idle session expiration |
| `maxSessions` | `1000` | Global client session capacity |
| `maxRequestBytes` | `1048576` | Maximum HTTP JSON body |
| `maxResponseBytes` | `4194304` | Maximum downstream response and upstream response envelope |
| `discoveryConcurrency` | `4` | Maximum simultaneous server discovery operations |
| `maxConcurrentRequests` | `128` | Maximum authenticated HTTP operations |
| `allowedOrigins` | `[]` | Exact allowed Origins; absent Origin is permitted |
| `servers` | `[]` | Configured downstream services |
| `principals` | `[]` | Service client policies; empty only allowed on loopback |
| `context` | disabled | Optional result transforms |

Server entries:

```json
{
  "name": "github",
  "url": "https://mcp.internal.example/mcp",
  "tokenEnv": "GITHUB_MCP_TOKEN",
  "timeout": "10s",
  "maxConcurrent": 16,
  "failureThreshold": 5,
  "cooldown": "15s"
}
```

Names use letters, numbers, underscores, or hyphens (1–40 characters). Public tool names are `server.originalToolName`, with a total limit of 128 characters. Upstream tool names are not altered during actual downstream calls.

Endpoints must be HTTP(S) URLs without embedded credentials, query strings, or fragments. Credentials require HTTPS except for literal loopback addresses used by local fixtures. Redirects are disabled. Only operator-configured URLs are contacted; enforce egress allowlists at your network boundary if operators are not fully trusted.

After `failureThreshold` transport failures, the circuit rejects work until `cooldown` expires. One probe is then admitted. Tool-level `isError` and JSON-RPC application errors do not trip the circuit. Discovery and execution share the backend capacity. Calls over capacity fail immediately, without an unbounded queue.

Principal entries:

```json
{
  "name": "reader",
  "tokenEnv": "READER_TOKEN",
  "allowTools": ["github.search", "health.check"],
  "denyTools": ["*.delete"],
  "allowServers": ["github", "local"],
  "requestsPerMinute": 120
}
```

Tokens must be unique and at least 32 characters. Empty `allowTools` grants no tool access. `denyTools` overrides allows. Patterns use Go `path.Match` semantics (`*`, `?`, character classes). Empty `allowServers` adds no restriction beyond the tool rules; the built-in health tool belongs to `local`.

Quotas use a per-principal token bucket with an initial burst equal to `requestsPerMinute`. The local anonymous development principal is limited to 120 HTTP operations/minute. Initialize, notifications, discovery, tool calls, and metrics scrapes all consume capacity. Quotas are per process, not distributed.

Context policy:

```json
{
  "maxOutputBytes": 16384,
  "maxEstimatedTokens": 4096,
  "redactKeys": ["password", "access_token"]
}
```

Zero budgets disable that limit. Minimum nonzero byte budget is 512; minimum estimated-token budget is 128. The token estimate uses four bytes/token and is not suitable for strict provider context limits. Redaction processes structured JSON and JSON in text blocks; use a separate content-security system for arbitrary prose, images, and encoded secrets. Truncation is explicit and never silently reports a failed tool as successful.
