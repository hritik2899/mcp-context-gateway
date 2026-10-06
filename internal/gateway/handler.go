// Package gateway implements the client-facing, session-bound MCP HTTP endpoint.
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hritik2899/mcp-context-gateway/internal/mcp"
)

type Catalog interface {
	List(context.Context) ([]mcp.ToolDefinition, error)
}
type Executor interface {
	Execute(context.Context, string, map[string]any) (any, error)
}
type Options struct {
	MaxBodyBytes   int64
	RequestTimeout time.Duration
	SessionTTL     time.Duration
	MaxSessions    int
}
type session struct {
	owner       string
	initialized bool
	expires     time.Time
	active      map[string]context.CancelFunc
}
type Handler struct {
	catalog  Catalog
	executor Executor
	options  Options
	mu       sync.Mutex
	sessions map[string]*session
}
type identityKey struct{}

func WithIdentity(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, identityKey{}, name)
}
func Identity(ctx context.Context) string { v, _ := ctx.Value(identityKey{}).(string); return v }
func NewHandler(c Catalog, e Executor, o Options) *Handler {
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = 1 << 20
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = 30 * time.Second
	}
	if o.SessionTTL <= 0 {
		o.SessionTTL = time.Hour
	}
	if o.MaxSessions <= 0 {
		o.MaxSessions = 1000
	}
	return &Handler{catalog: c, executor: e, options: o, sessions: map[string]*session{}}
}

var numberID = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	var text string
	return json.Unmarshal(id, &text) == nil || numberID.Match(bytes.TrimSpace(id))
}
func accepts(header, media string) bool {
	for _, part := range strings.Split(header, ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err == nil && kind == media && params["q"] != "0" {
			return true
		}
	}
	return false
}
func write(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *mcp.JSONRPCError) {
	data, err := json.Marshal(mcp.JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr})
	if err != nil {
		data, _ = json.Marshal(mcp.JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: &mcp.JSONRPCError{Code: mcp.InternalError, Message: "cannot encode result"}})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
func fail(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	write(w, id, nil, &mcp.JSONRPCError{Code: code, Message: message})
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && version != mcp.ProtocolVersion {
		http.Error(w, "unsupported MCP protocol version", 400)
		return
	}
	if r.Method == http.MethodDelete {
		h.deleteSession(w, r)
		return
	}
	kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if kind != "application/json" {
		http.Error(w, "Content-Type must be application/json", 415)
		return
	}
	if !accepts(r.Header.Get("Accept"), "application/json") || !accepts(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "Accept must include application/json and text/event-stream", 406)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.options.MaxBodyBytes))
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			http.Error(w, "request body too large", 413)
		} else {
			fail(w, nil, mcp.ParseError, "cannot read JSON")
		}
		return
	}
	if !json.Valid(data) {
		fail(w, nil, mcp.ParseError, "invalid JSON")
		return
	}
	var request mcp.JSONRPCRequest
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' || json.Unmarshal(data, &request) != nil || request.JSONRPC != "2.0" || request.Method == "" {
		fail(w, nil, mcp.InvalidRequest, "invalid JSON-RPC request")
		return
	}
	if len(request.ID) > 0 && !validID(request.ID) {
		fail(w, nil, mcp.InvalidRequest, "id must be a string or integer")
		return
	}
	if len(request.Params) > 0 && (len(bytes.TrimSpace(request.Params)) == 0 || bytes.TrimSpace(request.Params)[0] != '{') {
		if len(request.ID) == 0 {
			w.WriteHeader(202)
		} else {
			fail(w, request.ID, mcp.InvalidParams, "params must be an object")
		}
		return
	}
	if request.Method == mcp.InitializeMethod && len(request.ID) > 0 {
		h.initialize(w, r, request)
		return
	}
	sid := r.Header.Get("MCP-Session-Id")
	h.mu.Lock()
	h.expireLocked(time.Now())
	s := h.sessions[sid]
	if s == nil || s.owner != Identity(r.Context()) {
		h.mu.Unlock()
		http.Error(w, "unknown MCP session", 404)
		return
	}
	s.expires = time.Now().Add(h.options.SessionTTL)
	if len(request.ID) == 0 {
		switch request.Method {
		case mcp.InitializedNotification:
			s.initialized = true
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(request.Params, &p) == nil {
				if cancel := s.active[string(p.RequestID)]; cancel != nil {
					cancel()
				}
			}
		}
		h.mu.Unlock()
		w.WriteHeader(202)
		return
	}
	if !s.initialized && request.Method != "ping" {
		h.mu.Unlock()
		fail(w, request.ID, mcp.InvalidRequest, "session is not initialized")
		return
	}
	key := string(request.ID)
	if _, exists := s.active[key]; exists {
		h.mu.Unlock()
		fail(w, request.ID, mcp.InvalidRequest, "duplicate in-flight request id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.options.RequestTimeout)
	s.active[key] = cancel
	h.mu.Unlock()
	defer func() { cancel(); h.mu.Lock(); delete(s.active, key); h.mu.Unlock() }()
	switch request.Method {
	case "ping":
		write(w, request.ID, map[string]any{}, nil)
	case mcp.ToolsListMethod:
		// A complete bounded catalog is returned in one page; stale cursors are rejected.
		var p struct {
			Cursor string `json:"cursor"`
		}
		if len(request.Params) > 0 && (json.Unmarshal(request.Params, &p) != nil || p.Cursor != "") {
			fail(w, request.ID, mcp.InvalidParams, "invalid cursor")
			return
		}
		definitions, err := h.catalog.List(ctx)
		if err != nil {
			h.writeExecutionError(w, request.ID, err)
			return
		}
		if definitions == nil {
			definitions = []mcp.ToolDefinition{}
		}
		write(w, request.ID, mcp.ToolsListResult{Tools: definitions}, nil)
	case mcp.ToolsCallMethod:
		var p mcp.CallToolParams
		if json.Unmarshal(request.Params, &p) != nil || p.Name == "" {
			fail(w, request.ID, mcp.InvalidParams, "tool name and object arguments are required")
			return
		}
		result, err := h.executor.Execute(ctx, p.Name, p.Arguments)
		if err != nil {
			h.writeExecutionError(w, request.ID, err)
			return
		}
		normalized, err := mcp.NormalizeResult(result)
		if err != nil {
			h.writeExecutionError(w, request.ID, err)
			return
		}
		write(w, request.ID, normalized, nil)
	default:
		fail(w, request.ID, mcp.MethodNotFound, "method not found")
	}
}
func (h *Handler) writeExecutionError(w http.ResponseWriter, id json.RawMessage, err error) {
	var rpc *mcp.JSONRPCError
	if errors.As(err, &rpc) {
		write(w, id, nil, rpc)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fail(w, id, mcp.InternalError, "request deadline exceeded")
		return
	}
	if errors.Is(err, context.Canceled) {
		fail(w, id, mcp.InternalError, "request cancelled")
		return
	}
	fail(w, id, mcp.InternalError, "tool backend unavailable")
}
func (h *Handler) initialize(w http.ResponseWriter, r *http.Request, request mcp.JSONRPCRequest) {
	var p mcp.InitializeParams
	if json.Unmarshal(request.Params, &p) != nil || p.ProtocolVersion == "" || p.ClientInfo.Name == "" || p.ClientInfo.Version == "" || p.Capabilities == nil {
		fail(w, request.ID, mcp.InvalidParams, "protocolVersion, capabilities and clientInfo are required")
		return
	}
	if r.Header.Get("MCP-Session-Id") != "" {
		fail(w, request.ID, mcp.InvalidRequest, "initialize must create a new session")
		return
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		fail(w, request.ID, mcp.InternalError, "cannot create session")
		return
	}
	sid := hex.EncodeToString(token[:])
	h.mu.Lock()
	h.expireLocked(time.Now())
	if len(h.sessions) >= h.options.MaxSessions {
		h.mu.Unlock()
		http.Error(w, "session capacity reached", 503)
		return
	}
	h.sessions[sid] = &session{owner: Identity(r.Context()), expires: time.Now().Add(h.options.SessionTTL), active: map[string]context.CancelFunc{}}
	h.mu.Unlock()
	w.Header().Set("MCP-Session-Id", sid)
	write(w, request.ID, mcp.InitializeResult{ProtocolVersion: mcp.ProtocolVersion, Capabilities: map[string]any{"tools": map[string]any{}}, ServerInfo: mcp.ServerInfo{Name: "mcp-context-gateway", Version: "0.2.0"}}, nil)
}
func (h *Handler) expireLocked(now time.Time) {
	for id, s := range h.sessions {
		if now.After(s.expires) {
			for _, cancel := range s.active {
				cancel()
			}
			delete(h.sessions, id)
		}
	}
}
func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := r.Header.Get("MCP-Session-Id")
	s := h.sessions[id]
	if s == nil || s.owner != Identity(r.Context()) {
		http.Error(w, "unknown MCP session", 404)
		return
	}
	for _, cancel := range s.active {
		cancel()
	}
	delete(h.sessions, id)
	w.WriteHeader(204)
}

// Close cancels every active request during process shutdown.
func (h *Handler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, s := range h.sessions {
		for _, cancel := range s.active {
			cancel()
		}
		delete(h.sessions, id)
	}
}
