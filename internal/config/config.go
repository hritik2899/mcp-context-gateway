// Package config loads strict JSON configuration. Credentials are environment references.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("duration must be a string")
	}
	value, err := time.ParseDuration(s)
	*d = Duration(value)
	return err
}
func (d Duration) Value() time.Duration { return time.Duration(d) }

type Server struct {
	Name             string   `json:"name"`
	URL              string   `json:"url"`
	TokenEnv         string   `json:"tokenEnv,omitempty"`
	Timeout          Duration `json:"timeout"`
	MaxConcurrent    int      `json:"maxConcurrent"`
	FailureThreshold int      `json:"failureThreshold"`
	Cooldown         Duration `json:"cooldown"`
	Token            string   `json:"-"`
}
type Principal struct {
	Name              string   `json:"name"`
	TokenEnv          string   `json:"tokenEnv"`
	AllowTools        []string `json:"allowTools"`
	DenyTools         []string `json:"denyTools,omitempty"`
	AllowServers      []string `json:"allowServers,omitempty"`
	RequestsPerMinute int      `json:"requestsPerMinute"`
	Token             string   `json:"-"`
}
type ContextPolicy struct {
	MaxOutputBytes int      `json:"maxOutputBytes"`
	RedactKeys     []string `json:"redactKeys"`
}
type Config struct {
	Listen                string        `json:"listen"`
	RequestTimeout        Duration      `json:"requestTimeout"`
	DiscoveryTimeout      Duration      `json:"discoveryTimeout"`
	RefreshInterval       Duration      `json:"refreshInterval"`
	ShutdownTimeout       Duration      `json:"shutdownTimeout"`
	SessionTTL            Duration      `json:"sessionTTL"`
	MaxSessions           int           `json:"maxSessions"`
	MaxRequestBytes       int64         `json:"maxRequestBytes"`
	MaxResponseBytes      int64         `json:"maxResponseBytes"`
	DiscoveryConcurrency  int           `json:"discoveryConcurrency"`
	MaxConcurrentRequests int           `json:"maxConcurrentRequests"`
	AllowedOrigins        []string      `json:"allowedOrigins"`
	Servers               []Server      `json:"servers"`
	Principals            []Principal   `json:"principals"`
	Context               ContextPolicy `json:"context"`
}

func Default() Config {
	return Config{Listen: "127.0.0.1:8080", RequestTimeout: Duration(30 * time.Second), DiscoveryTimeout: Duration(10 * time.Second), RefreshInterval: Duration(30 * time.Second), ShutdownTimeout: Duration(10 * time.Second), SessionTTL: Duration(time.Hour), MaxSessions: 1000, MaxRequestBytes: 1 << 20, MaxResponseBytes: 4 << 20, DiscoveryConcurrency: 4, MaxConcurrentRequests: 128}
}
func Load(filename string) (Config, error) {
	c := Default()
	if filename != "" {
		f, err := os.Open(filename)
		if err != nil {
			return c, err
		}
		defer f.Close()
		decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&c); err != nil {
			return c, err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return c, fmt.Errorf("configuration must contain exactly one JSON object")
		}
	}
	return c, c.Validate()
}

var name = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

func (c *Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || port == "" {
		return fmt.Errorf("listen must be host:port")
	}
	ip := net.ParseIP(host)
	if len(c.Principals) == 0 && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("non-loopback binding requires configured principals")
	}
	for _, duration := range []Duration{c.RequestTimeout, c.DiscoveryTimeout, c.RefreshInterval, c.ShutdownTimeout, c.SessionTTL} {
		if duration <= 0 {
			return fmt.Errorf("timeouts and intervals must be positive")
		}
	}
	if c.MaxSessions <= 0 || c.MaxRequestBytes <= 0 || c.MaxResponseBytes < 1024 || c.DiscoveryConcurrency <= 0 || c.MaxConcurrentRequests <= 0 {
		return fmt.Errorf("capacity limits must be positive (response bytes >= 1024)")
	}
	if c.MaxRequestBytes > 64<<20 || c.MaxResponseBytes > 64<<20 {
		return fmt.Errorf("body limits must not exceed 64 MiB")
	}
	seen := map[string]bool{}
	for i := range c.Servers {
		s := &c.Servers[i]
		if !name.MatchString(s.Name) || seen[s.Name] {
			return fmt.Errorf("invalid or duplicate server name %q", s.Name)
		}
		seen[s.Name] = true
		u, err := url.Parse(s.URL)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("server %q requires an HTTP(S) URL without credentials, query, or fragment", s.Name)
		}
		if s.Timeout == 0 {
			s.Timeout = Duration(10 * time.Second)
		}
		if s.MaxConcurrent == 0 {
			s.MaxConcurrent = 16
		}
		if s.FailureThreshold == 0 {
			s.FailureThreshold = 5
		}
		if s.Cooldown == 0 {
			s.Cooldown = Duration(15 * time.Second)
		}
		if s.Timeout < 0 || s.MaxConcurrent < 1 || s.FailureThreshold < 1 || s.Cooldown < 0 {
			return fmt.Errorf("invalid limits for server %q", s.Name)
		}
		if s.TokenEnv != "" {
			s.Token = os.Getenv(s.TokenEnv)
			if s.Token == "" {
				return fmt.Errorf("credential environment variable for server %q is empty", s.Name)
			}
			if u.Scheme != "https" {
				ip := net.ParseIP(u.Hostname())
				if ip == nil || !ip.IsLoopback() {
					return fmt.Errorf("server %q credentials require HTTPS outside loopback", s.Name)
				}
			}
		}
	}
	seen = map[string]bool{}
	tokens := map[string]bool{}
	for i := range c.Principals {
		p := &c.Principals[i]
		if !name.MatchString(p.Name) || seen[p.Name] {
			return fmt.Errorf("invalid or duplicate principal name")
		}
		seen[p.Name] = true
		p.Token = os.Getenv(p.TokenEnv)
		if len(p.Token) < 32 || tokens[p.Token] || strings.ContainsAny(p.Token, "\r\n") {
			return fmt.Errorf("principal %q requires a unique token of at least 32 characters in its environment variable", p.Name)
		}
		tokens[p.Token] = true
		for _, pattern := range append(append(append([]string{}, p.AllowTools...), p.DenyTools...), p.AllowServers...) {
			if _, err := path.Match(pattern, ""); err != nil {
				return fmt.Errorf("invalid policy pattern for %q", p.Name)
			}
		}
		if p.RequestsPerMinute == 0 {
			p.RequestsPerMinute = 120
		}
		if p.RequestsPerMinute < 1 {
			return fmt.Errorf("rate limit must be positive")
		}
	}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("invalid allowed origin")
		}
	}
	if c.Context.MaxOutputBytes < 0 || (c.Context.MaxOutputBytes > 0 && c.Context.MaxOutputBytes < 256) {
		return fmt.Errorf("context maxOutputBytes must be zero or at least 256")
	}
	return nil
}

// JSON emits a credential-free diagnostic representation.
func (c Config) JSON() []byte {
	data, _ := json.MarshalIndent(c, "", "  ")
	return bytes.TrimSpace(data)
}
