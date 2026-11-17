package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ServerConfig defines a configured MCP server (stdio subprocess or Streamable HTTP).
type ServerConfig struct {
	Name     string            `json:"-"`
	Type     string            `json:"type,omitempty"`
	Command  string            `json:"command,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	URL      string            `json:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
}

// NotFoundError indicates a requested MCP server was not found in the config store.
type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("mcp: server %q not found", e.Name)
}

type configFile struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ConfigStore persists MCP server configurations in <dataDir>/mcp_config.json with 0600 permissions.
type ConfigStore struct {
	path    string
	mu      sync.RWMutex
	loaded  bool
	servers map[string]ServerConfig
}

// NewConfigStore creates a ConfigStore backed by the given file path.
func NewConfigStore(path string) *ConfigStore {
	return &ConfigStore{
		path:    path,
		servers: make(map[string]ServerConfig),
	}
}

// Load reads server configurations from disk. Missing files are treated as empty.
func (s *ConfigStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *ConfigStore) loadLocked() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.servers = make(map[string]ServerConfig)
			s.loaded = true
			return nil
		}
		return fmt.Errorf("mcp: read config %s: %w", s.path, err)
	}

	var cf configFile
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cf); err != nil {
			return fmt.Errorf("mcp: parse config %s: %w", s.path, err)
		}
	}

	servers := make(map[string]ServerConfig, len(cf.MCPServers))
	for name, cfg := range cf.MCPServers {
		cfg.Name = name
		if cfg.Type == "" {
			if cfg.URL != "" {
				cfg.Type = "http"
			} else if cfg.Command != "" {
				cfg.Type = "stdio"
			}
		}
		servers[name] = cfg
	}
	s.servers = servers
	s.loaded = true
	return nil
}

// Save writes the current server configurations to disk with 0600 permissions.
func (s *ConfigStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *ConfigStore) saveLocked() error {
	if s.path == "" {
		return errors.New("mcp: config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("mcp: create config dir: %w", err)
	}

	cf := configFile{
		MCPServers: make(map[string]ServerConfig, len(s.servers)),
	}
	for name, cfg := range s.servers {
		cf.MCPServers[name] = cfg
	}

	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp: marshal config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("mcp: write config %s: %w", s.path, err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("mcp: chmod config %s: %w", s.path, err)
	}
	return nil
}

// Add validates and stores a server configuration, persisting to disk.
func (s *ConfigStore) Add(cfg ServerConfig) error {
	if cfg.Name == "" {
		return errors.New("mcp: server name is required")
	}
	if cfg.Type == "" {
		switch {
		case cfg.URL != "":
			cfg.Type = "http"
		case cfg.Command != "":
			cfg.Type = "stdio"
		}
	}
	switch cfg.Type {
	case "stdio":
		if cfg.Command == "" {
			return fmt.Errorf("mcp: stdio server %q requires command", cfg.Name)
		}
	case "http":
		if cfg.URL == "" {
			return fmt.Errorf("mcp: http server %q requires url", cfg.Name)
		}
	default:
		return fmt.Errorf("mcp: unsupported server type %q (want \"stdio\" or \"http\")", cfg.Type)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		if err := s.loadLocked(); err != nil {
			return err
		}
	}
	s.servers[cfg.Name] = cloneServerConfig(cfg)
	return s.saveLocked()
}

// Remove deletes a server configuration by name and persists the change.
func (s *ConfigStore) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		if err := s.loadLocked(); err != nil {
			return err
		}
	}
	if _, ok := s.servers[name]; !ok {
		return &NotFoundError{Name: name}
	}
	delete(s.servers, name)
	return s.saveLocked()
}

// SetEnabled enables or disables a configured server by name and persists the change.
func (s *ConfigStore) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		if err := s.loadLocked(); err != nil {
			return err
		}
	}
	cfg, ok := s.servers[name]
	if !ok {
		return &NotFoundError{Name: name}
	}
	cfg.Disabled = !enabled
	s.servers[name] = cfg
	return s.saveLocked()
}

// List returns all configured servers sorted by name.
func (s *ConfigStore) List() []ServerConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ServerConfig, 0, len(s.servers))
	for _, cfg := range s.servers {
		out = append(out, cloneServerConfig(cfg))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func cloneServerConfig(cfg ServerConfig) ServerConfig {
	cp := cfg
	if cfg.Args != nil {
		cp.Args = append([]string(nil), cfg.Args...)
	}
	if cfg.Env != nil {
		cp.Env = make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			cp.Env[k] = v
		}
	}
	if cfg.Headers != nil {
		cp.Headers = make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			cp.Headers[k] = v
		}
	}
	return cp
}
