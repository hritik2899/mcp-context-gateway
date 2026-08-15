# MCP Context Gateway

A production-oriented gateway for managing MCP tool access and AI context across downstream services.

## Current milestone

The gateway starts as an independently runnable HTTP service with an explicit health contract. MCP protocol handling and downstream routing will be layered on top of this foundation rather than introduced as one large implementation.

## Run

```bash
go run ./cmd/gateway
```

Then:

```bash
curl http://localhost:8080/healthz
```

Expected response:

```json
{"status":"ok"}
```

## Architecture direction

```text
MCP Client
    |
    v
+-------------------+
| Context Gateway   |
|                   |
| MCP Transport     |
| Context Engine    |
| Tool Registry     |
| Router            |
| Resilience        |
+---------+---------+
          |
          v
   Downstream MCP
      Servers
```
