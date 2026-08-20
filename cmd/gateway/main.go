package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
	"github.com/hritik2899/mcp-context-gateway/internal/router"
	"github.com/hritik2899/mcp-context-gateway/internal/tools"
)

const serverVersion = "0.1.0"

func main() {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.Definition{
		Name:        "health.check",
		Description: "Returns the gateway health status.",
		InputSchema: map[string]any{"type": "object"},
	})

	executor := tools.NewLocalExecutor(registry)
	_ = executor.Register("health.check", func(ctx context.Context, arguments map[string]any) (any, error) {
		return map[string]any{"status": "ok"}, nil
	})

	executionRouter := router.New(executor)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		handleMCP(w, r, registry, executionRouter)
	})

	server := &http.Server{Addr: ":8080", Handler: mux}
	log.Printf("mcp-context-gateway listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func handleMCP(w http.ResponseWriter, r *http.Request, registry *tools.Registry, executionRouter router.Route) {
	var request mcp.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONRPCError(w, nil, mcp.InvalidRequest, "invalid JSON-RPC request")
		return
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		writeJSONRPCError(w, request.ID, mcp.InvalidRequest, "invalid JSON-RPC request")
		return
	}

	switch request.Method {
	case mcp.InitializeMethod:
		handleInitialize(w, request, registry)
	case mcp.InitializedNotification:
		w.WriteHeader(http.StatusAccepted)
	case mcp.ToolsListMethod:
		handleToolsList(w, request, registry)
	case mcp.ToolsCallMethod:
		handleToolCall(w, r, request, executionRouter)
	default:
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if request.Method == "ping" {
			writeJSON(w, mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: map[string]any{}})
			return
		}
		writeJSONRPCError(w, request.ID, mcp.MethodNotFound, "method not found")
	}
}

func handleToolsList(w http.ResponseWriter, request mcp.JSONRPCRequest, registry *tools.Registry) {
	writeJSON(w, mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  map[string]any{"tools": registry.List()},
	})
}

func handleToolCall(w http.ResponseWriter, r *http.Request, request mcp.JSONRPCRequest, executionRouter router.Route) {
	var params mcp.CallToolParams
	if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
		writeJSONRPCError(w, request.ID, mcp.InvalidParams, "tool name is required")
		return
	}

	result, err := executionRouter.Execute(r.Context(), params.Name, params.Arguments)
	if err != nil {
		writeJSONRPCError(w, request.ID, mcp.InternalError, err.Error())
		return
	}

	content, ok := result.(map[string]any)
	if !ok {
		content = map[string]any{"result": result}
	}
	text, _ := json.Marshal(content)
	writeJSON(w, mcp.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      request.ID,
		Result:  mcp.CallToolResult{Content: []mcp.ContentBlock{{Type: "text", Text: string(text)}}},
	})
}

func handleInitialize(w http.ResponseWriter, request mcp.JSONRPCRequest, registry *tools.Registry) {
	var params mcp.InitializeParams
	if err := json.Unmarshal(request.Params, &params); err != nil {
		writeJSONRPCError(w, request.ID, mcp.InvalidParams, "invalid initialize parameters")
		return
	}
	if params.ProtocolVersion == "" || params.ClientInfo.Name == "" {
		writeJSONRPCError(w, request.ID, mcp.InvalidParams, "protocolVersion and clientInfo are required")
		return
	}

	capabilities := map[string]any{}
	if len(registry.List()) > 0 {
		capabilities["tools"] = map[string]any{}
	}
	result := mcp.InitializeResult{
		ProtocolVersion: params.ProtocolVersion,
		Capabilities:    capabilities,
		ServerInfo:      mcp.ServerInfo{Name: "mcp-context-gateway", Version: serverVersion},
	}
	writeJSON(w, mcp.JSONRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: result})
}

func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, mcp.JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &mcp.JSONRPCError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, response mcp.JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
