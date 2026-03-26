package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the top-level YAML configuration.
type Config struct {
	Mode           string           `yaml:"mode"`             // "probe" (default) or "crawl"
	CrawlFile      string           `yaml:"crawl-file"`       // crawl mode: output file for discovered ENRs
	KeyDir         string           `yaml:"key-dir"`          // directory for per-instance key files
	MetricsAddr    string           `yaml:"metrics-addr"`     // Prometheus metrics listen address
	BaseTCPPort    uint             `yaml:"base-tcp-port"`    // base TCP port; instance i gets base + 2*i
	BaseQUICPort   uint             `yaml:"base-quic-port"`   // base QUIC port; instance i gets base + 2*i
	BaseDiscV5Port uint             `yaml:"base-discv5-port"` // base discv5 port; instance i gets base + i
	DiscV4Port     uint             `yaml:"discv4-port"`      // shared discv4 port (0 to disable)
	Subnets        []uint64         `yaml:"subnets"`          // attestation subnet IDs
	ListenBlocks   bool             `yaml:"listen-blocks"`    // subscribe to beacon block topic (first instance only)
	BootstrapFile  string           `yaml:"bootstrap-file"`   // path to ENR file for direct-dialing peers
	LogLevel       string           `yaml:"log-level"`        // log level (debug, info, warn, error)
	Instances      []InstanceConfig `yaml:"instances"`        // per-instance configuration
}

// InstanceConfig holds per-instance configuration.
type InstanceConfig struct {
	Name             string `yaml:"name"`              // unique instance name (used in metrics probe label)
	GossipD          int    `yaml:"gossip-d"`          // GossipSub mesh degree
	LogFile          string `yaml:"log-file"`          // attestation arrival log file path
	GossipSubLogFile string `yaml:"gossipsub-log-file"` // GossipSub event log file path
	DisableIHave     bool   `yaml:"disable-ihave"`     // disable IHAVE gossip
	MaxPeers         int    `yaml:"max-peers"`         // max peers (0 = 10*D)
	QuicOnly         bool   `yaml:"quic-only"`         // only use QUIC transport
}

// Load reads and parses a YAML config file, applies defaults, and validates.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Mode:        "probe",
		MetricsAddr: ":9090",
		LogLevel:    "info",
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	for i := range cfg.Instances {
		if cfg.Instances[i].MaxPeers == 0 && cfg.Instances[i].GossipD > 0 {
			cfg.Instances[i].MaxPeers = 10 * cfg.Instances[i].GossipD
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for consistency.
func (c *Config) Validate() error {
	if c.Mode != "probe" && c.Mode != "crawl" {
		return fmt.Errorf("mode must be \"probe\" or \"crawl\", got %q", c.Mode)
	}

	if len(c.Instances) == 0 {
		return fmt.Errorf("at least one instance is required")
	}

	// Unique instance names.
	names := make(map[string]struct{}, len(c.Instances))
	for _, inst := range c.Instances {
		if inst.Name == "" {
			return fmt.Errorf("instance name is required")
		}
		if _, ok := names[inst.Name]; ok {
			return fmt.Errorf("duplicate instance name: %q", inst.Name)
		}
		names[inst.Name] = struct{}{}
	}

	// Crawl mode constraints.
	if c.Mode == "crawl" {
		if len(c.Instances) != 1 {
			return fmt.Errorf("crawl mode requires exactly 1 instance, got %d", len(c.Instances))
		}
		if c.CrawlFile == "" {
			return fmt.Errorf("crawl mode requires crawl-file")
		}
		if c.BootstrapFile != "" {
			return fmt.Errorf("crawl mode and bootstrap-file are mutually exclusive")
		}
	}

	// Probe mode constraints.
	if c.Mode == "probe" {
		for _, inst := range c.Instances {
			if inst.LogFile == "" {
				return fmt.Errorf("instance %q: log-file is required in probe mode", inst.Name)
			}
			if inst.GossipD <= 0 {
				return fmt.Errorf("instance %q: gossip-d must be positive", inst.Name)
			}
		}
	}

	// Multi-instance requires non-zero base ports so collision detection works.
	if len(c.Instances) > 1 {
		if c.BaseTCPPort == 0 || c.BaseQUICPort == 0 || c.BaseDiscV5Port == 0 {
			return fmt.Errorf("base-tcp-port, base-quic-port, and base-discv5-port must be non-zero with multiple instances")
		}
	}

	// Check for port collisions across all instances and port types.
	ports := make(map[uint]string) // port → description
	addPort := func(port uint, desc string) error {
		if port == 0 {
			return nil
		}
		if existing, ok := ports[port]; ok {
			return fmt.Errorf("port %d collision: %s and %s", port, existing, desc)
		}
		ports[port] = desc
		return nil
	}

	for i, inst := range c.Instances {
		if err := addPort(c.TCPPort(i), fmt.Sprintf("%s/tcp", inst.Name)); err != nil {
			return err
		}
		if err := addPort(c.QUICPort(i), fmt.Sprintf("%s/quic", inst.Name)); err != nil {
			return err
		}
		if err := addPort(c.DiscV5Port(i), fmt.Sprintf("%s/discv5", inst.Name)); err != nil {
			return err
		}
	}
	if err := addPort(c.DiscV4Port, "discv4"); err != nil {
		return err
	}

	return nil
}

// TCPPort returns the TCP port for instance i.
func (c *Config) TCPPort(i int) uint {
	return c.BaseTCPPort + 2*uint(i)
}

// QUICPort returns the QUIC port for instance i.
func (c *Config) QUICPort(i int) uint {
	return c.BaseQUICPort + 2*uint(i)
}

// DiscV5Port returns the discv5 port for instance i.
func (c *Config) DiscV5Port(i int) uint {
	return c.BaseDiscV5Port + uint(i)
}

// KeyFile returns the key file path for instance i.
func (c *Config) KeyFile(i int) string {
	return filepath.Join(c.KeyDir, c.Instances[i].Name+".key")
}
