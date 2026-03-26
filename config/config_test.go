package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBasic(t *testing.T) {
	yaml := `
mode: probe
key-dir: ./keys
metrics-addr: ":9090"
base-tcp-port: 9020
base-quic-port: 9021
base-discv5-port: 9000
subnets: [0, 1, 2, 3]
listen-blocks: true
log-level: info
instances:
  - name: d6
    gossip-d: 6
    log-file: /tmp/att-d6.log
    gossipsub-log-file: /tmp/gs-d6.log
  - name: d12
    gossip-d: 12
    log-file: /tmp/att-d12.log
    gossipsub-log-file: /tmp/gs-d12.log
    disable-ihave: true
    quic-only: true
`
	cfg := loadFromString(t, yaml)

	if cfg.Mode != "probe" {
		t.Fatalf("mode = %q, want probe", cfg.Mode)
	}
	if len(cfg.Instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(cfg.Instances))
	}
	if cfg.Instances[0].Name != "d6" {
		t.Fatalf("instance[0].name = %q, want d6", cfg.Instances[0].Name)
	}
	if cfg.Instances[1].DisableIHave != true {
		t.Fatalf("instance[1].disable-ihave = false, want true")
	}
	if cfg.Instances[1].QuicOnly != true {
		t.Fatalf("instance[1].quic-only = false, want true")
	}
}

func TestDefaultMaxPeers(t *testing.T) {
	yaml := `
mode: probe
base-tcp-port: 9020
base-quic-port: 9021
base-discv5-port: 9000
instances:
  - name: d6
    gossip-d: 6
    log-file: /tmp/att.log
  - name: d12
    gossip-d: 12
    log-file: /tmp/att2.log
    max-peers: 200
`
	cfg := loadFromString(t, yaml)

	if cfg.Instances[0].MaxPeers != 60 {
		t.Fatalf("instance[0].max-peers = %d, want 60 (10*6)", cfg.Instances[0].MaxPeers)
	}
	if cfg.Instances[1].MaxPeers != 200 {
		t.Fatalf("instance[1].max-peers = %d, want 200 (explicit)", cfg.Instances[1].MaxPeers)
	}
}

func TestPortFormulas(t *testing.T) {
	cfg := &Config{
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
	}

	tests := []struct {
		idx      int
		wantTCP  uint
		wantQUIC uint
		wantDisc uint
	}{
		{0, 9020, 9021, 9000},
		{1, 9022, 9023, 9001},
		{2, 9024, 9025, 9002},
	}

	for _, tt := range tests {
		if got := cfg.TCPPort(tt.idx); got != tt.wantTCP {
			t.Errorf("TCPPort(%d) = %d, want %d", tt.idx, got, tt.wantTCP)
		}
		if got := cfg.QUICPort(tt.idx); got != tt.wantQUIC {
			t.Errorf("QUICPort(%d) = %d, want %d", tt.idx, got, tt.wantQUIC)
		}
		if got := cfg.DiscV5Port(tt.idx); got != tt.wantDisc {
			t.Errorf("DiscV5Port(%d) = %d, want %d", tt.idx, got, tt.wantDisc)
		}
	}
}

func TestKeyFile(t *testing.T) {
	cfg := &Config{
		KeyDir: "/tmp/keys",
		Instances: []InstanceConfig{
			{Name: "d6"},
			{Name: "d12"},
		},
	}

	if got := cfg.KeyFile(0); got != "/tmp/keys/d6.key" {
		t.Fatalf("KeyFile(0) = %q, want /tmp/keys/d6.key", got)
	}
	if got := cfg.KeyFile(1); got != "/tmp/keys/d12.key" {
		t.Fatalf("KeyFile(1) = %q, want /tmp/keys/d12.key", got)
	}
}

func TestValidateDuplicateNames(t *testing.T) {
	cfg := &Config{
		Mode:           "probe",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances: []InstanceConfig{
			{Name: "d6", LogFile: "a.log"},
			{Name: "d6", LogFile: "b.log"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestValidateNoInstances(t *testing.T) {
	cfg := &Config{Mode: "probe"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for no instances")
	}
}

func TestValidateEmptyName(t *testing.T) {
	cfg := &Config{
		Mode:           "probe",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances:      []InstanceConfig{{Name: "", LogFile: "a.log"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateInvalidMode(t *testing.T) {
	cfg := &Config{
		Mode:      "invalid",
		Instances: []InstanceConfig{{Name: "a"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestValidateCrawlMode(t *testing.T) {
	// Crawl mode with 2 instances.
	cfg := &Config{
		Mode:           "crawl",
		CrawlFile:      "/tmp/peers.enr",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances: []InstanceConfig{
			{Name: "a"},
			{Name: "b"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for crawl mode with 2 instances")
	}

	// Crawl mode without crawl-file.
	cfg = &Config{
		Mode:           "crawl",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances:      []InstanceConfig{{Name: "a"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for crawl mode without crawl-file")
	}

	// Crawl mode with bootstrap-file.
	cfg = &Config{
		Mode:           "crawl",
		CrawlFile:      "/tmp/peers.enr",
		BootstrapFile:  "/tmp/bootstrap.enr",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances:      []InstanceConfig{{Name: "a"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for crawl mode with bootstrap-file")
	}

	// Valid crawl mode.
	cfg = &Config{
		Mode:           "crawl",
		CrawlFile:      "/tmp/peers.enr",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances:      []InstanceConfig{{Name: "crawler"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error for valid crawl config: %v", err)
	}
}

func TestValidateProbeRequiresLogFile(t *testing.T) {
	cfg := &Config{
		Mode:           "probe",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9000,
		Instances:      []InstanceConfig{{Name: "d6"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for probe mode without log-file")
	}
}

func TestValidatePortCollision(t *testing.T) {
	// discv5 port 9001 collides with instance 0's QUIC port 9021?
	// Actually let's make a real collision: base-tcp=9000, base-discv5=9000.
	cfg := &Config{
		Mode:           "probe",
		BaseTCPPort:    9000,
		BaseQUICPort:   9001,
		BaseDiscV5Port: 9000, // collides with TCP port of instance 0
		Instances:      []InstanceConfig{{Name: "a", LogFile: "a.log"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for port collision")
	}
}

func TestValidatePortCollisionCrossInstance(t *testing.T) {
	// Instance 0: TCP=9020, QUIC=9021. Instance 1: TCP=9022, QUIC=9023.
	// discv5: instance 0=9022 (collides with instance 1 TCP).
	cfg := &Config{
		Mode:           "probe",
		BaseTCPPort:    9020,
		BaseQUICPort:   9021,
		BaseDiscV5Port: 9022, // instance 0 discv5=9022, same as instance 1 TCP
		Instances: []InstanceConfig{
			{Name: "a", LogFile: "a.log"},
			{Name: "b", LogFile: "b.log"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for cross-instance port collision")
	}
}

func TestDefaultMode(t *testing.T) {
	yaml := `
base-tcp-port: 9020
base-quic-port: 9021
base-discv5-port: 9000
instances:
  - name: d6
    gossip-d: 6
    log-file: /tmp/att.log
`
	cfg := loadFromString(t, yaml)
	if cfg.Mode != "probe" {
		t.Fatalf("mode = %q, want probe (default)", cfg.Mode)
	}
}

func loadFromString(t *testing.T, content string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
