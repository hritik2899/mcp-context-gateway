package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Client interface {
	CallTool(context.Context, string, map[string]any) (CallToolResult, error)
}
type ClientOptions struct {
	Timeout          time.Duration
	MaxResponseBytes int64
	BearerToken      string
}

var ErrSessionExpired = errors.New("downstream session expired; retry only if operation is safe")

type HTTPClient struct {
	endpoint     string
	http         *http.Client
	options      ClientOptions
	sequence     atomic.Uint64
	mu           sync.Mutex
	session      string
	initialized  bool
	initializing chan struct{}
	closed       bool
}

func NewHTTPClient(endpoint string) *HTTPClient {
	return NewHTTPClientWithOptions(endpoint, ClientOptions{})
}
func NewHTTPClientWithOptions(endpoint string, options ClientOptions) *HTTPClient {
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Second
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = 4 << 20
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = options.Timeout
	return &HTTPClient{endpoint: endpoint, options: options, http: &http.Client{Transport: transport, Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("downstream redirects are disabled") }}}
}
func (c *HTTPClient) ensureInitialized(ctx context.Context) error {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return errors.New("downstream client closed")
		}
		if c.initialized {
			c.mu.Unlock()
			return nil
		}
		if ready := c.initializing; ready != nil {
			c.mu.Unlock()
			select {
			case <-ready:
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		ready := make(chan struct{})
		c.initializing = ready
		c.mu.Unlock()
		params := InitializeParams{ProtocolVersion: ProtocolVersion, Capabilities: map[string]any{}, ClientInfo: ClientInfo{Name: "mcp-context-gateway", Version: "0.2.0"}}
		raw, header, err := c.exchange(ctx, InitializeMethod, params, "", false)
		var result InitializeResult
		if err == nil {
			err = json.Unmarshal(raw, &result)
		}
		if err == nil && result.ProtocolVersion != ProtocolVersion {
			err = fmt.Errorf("unsupported downstream protocol version %q", result.ProtocolVersion)
		}
		if err == nil {
			if _, ok := result.Capabilities["tools"]; !ok {
				err = errors.New("downstream does not advertise tools")
			}
		}
		sid := header.Get("MCP-Session-Id")
		if err == nil {
			_, _, err = c.exchange(ctx, InitializedNotification, nil, sid, true)
		}
		c.mu.Lock()
		if err == nil && !c.closed {
			c.session = sid
			c.initialized = true
		}
		c.initializing = nil
		close(ready)
		c.mu.Unlock()
		return err
	}
}
func (c *HTTPClient) currentSession() string { c.mu.Lock(); defer c.mu.Unlock(); return c.session }
func (c *HTTPClient) invalidate(sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == sid {
		c.initialized = false
		c.session = ""
	}
}
func (c *HTTPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (CallToolResult, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return CallToolResult{}, err
	}
	sid := c.currentSession()
	raw, _, err := c.exchange(ctx, ToolsCallMethod, CallToolParams{Name: name, Arguments: arguments}, sid, false)
	if errors.Is(err, ErrSessionExpired) {
		c.invalidate(sid)
	}
	// Never replay a tool invocation: a failed response can follow a completed write.
	if err != nil {
		return CallToolResult{}, err
	}
	var result CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("invalid tool result: %w", err)
	}
	return result, nil
}
func (c *HTTPClient) exchange(ctx context.Context, method string, params any, sid string, notification bool) (json.RawMessage, http.Header, error) {
	var id json.RawMessage
	if !notification {
		id = json.RawMessage(strconv.FormatUint(c.sequence.Add(1), 10))
	}
	var encodedParams json.RawMessage
	var err error
	if params != nil {
		encodedParams, err = json.Marshal(params)
		if err != nil {
			return nil, nil, err
		}
	}
	body, err := json.Marshal(JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: encodedParams})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("invalid downstream endpoint")
	}
	c.headers(req, sid, method != InitializeMethod)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			if method == ToolsCallMethod {
				c.cancelDownstream(sid, id)
			}
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("downstream HTTP request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 && sid != "" {
		return nil, resp.Header, ErrSessionExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("downstream returned HTTP %d", resp.StatusCode)
	}
	if notification {
		if resp.StatusCode != 202 {
			return nil, resp.Header, errors.New("downstream did not acknowledge notification")
		}
		return nil, resp.Header, nil
	}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	reader := &io.LimitedReader{R: resp.Body, N: c.options.MaxResponseBytes + 1}
	var raw json.RawMessage
	switch media {
	case "application/json":
		raw, err = io.ReadAll(reader)
	case "text/event-stream":
		raw, err = readSSE(reader, id, int(c.options.MaxResponseBytes))
	default:
		err = fmt.Errorf("unsupported downstream content type %q", media)
	}
	if reader.N <= 0 {
		return nil, resp.Header, errors.New("downstream response too large")
	}
	if err != nil {
		if ctx.Err() != nil {
			if method == ToolsCallMethod {
				c.cancelDownstream(sid, id)
			}
			return nil, resp.Header, ctx.Err()
		}
		return nil, resp.Header, err
	}
	var wire struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *JSONRPCError   `json:"error"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, resp.Header, errors.New("invalid downstream JSON-RPC response")
	}
	if wire.JSONRPC != "2.0" || !bytes.Equal(bytes.TrimSpace(wire.ID), id) || (len(wire.Result) > 0) == (wire.Error != nil) {
		return nil, resp.Header, errors.New("invalid downstream response envelope")
	}
	if wire.Error != nil {
		return nil, resp.Header, wire.Error
	}
	return wire.Result, resp.Header, nil
}
func (c *HTTPClient) headers(req *http.Request, sid string, negotiated bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if negotiated {
		req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	}
	if sid != "" {
		req.Header.Set("MCP-Session-Id", sid)
	}
	if c.options.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.options.BearerToken)
	}
}
func (c *HTTPClient) cancelDownstream(sid string, id json.RawMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.exchange(ctx, "notifications/cancelled", map[string]any{"requestId": id}, sid, true)
}

// readSSE ignores comments/notifications and stops at the matching response.
// Interrupted streams return an error, never an automatic replay of tools/call.
func readSSE(reader io.Reader, id json.RawMessage, max int) (json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), max)
	var data []string
	dispatch := func() (json.RawMessage, bool) {
		joined := strings.Join(data, "\n")
		data = nil
		if joined == "" {
			return nil, false
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal([]byte(joined), &envelope) == nil && envelope.Method == "" && bytes.Equal(bytes.TrimSpace(envelope.ID), id) {
			return json.RawMessage(joined), true
		}
		return nil, false
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if raw, ok := dispatch(); ok {
				return raw, nil
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read downstream event stream: %w", err)
	}
	if raw, ok := dispatch(); ok {
		return raw, nil
	}
	return nil, errors.New("event stream ended before matching response")
}
func (c *HTTPClient) Close(ctx context.Context) error {
	c.mu.Lock()
	c.closed = true
	sid := c.session
	c.session = ""
	c.initialized = false
	c.mu.Unlock()
	defer c.http.CloseIdleConnections()
	if sid == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint, nil)
	if err != nil {
		return err
	}
	c.headers(req, sid, true)
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("close downstream session failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != 404 && resp.StatusCode != 405 {
		return fmt.Errorf("close downstream HTTP %d", resp.StatusCode)
	}
	return nil
}
