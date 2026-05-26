package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type HookConfig struct {
	Command  string        `yaml:"command"`
	Timeout  time.Duration `yaml:"timeout"`
	CanAbort bool          `yaml:"can_abort"`
}

type LifecycleConfig struct {
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type SessionsConfig struct {
	MaxSessions     int           `yaml:"max_sessions"`
	TTL             time.Duration `yaml:"ttl"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

type SandboxConfig struct {
	AllowedCommands []string `yaml:"allowed_commands"`
	BlockedCommands []string `yaml:"blocked_commands"`
	AllowedPaths    []string `yaml:"allowed_paths"`
	BlockedPaths    []string `yaml:"blocked_paths"`
	MaxOutputSize   int      `yaml:"max_output_size"`
}

type SkillsConfig struct {
	ProjectDir string `yaml:"project_dir"`
	UserDir    string `yaml:"user_dir"`
}

type Config struct {
	Providers      map[string]ProviderConfig `yaml:"providers"`
	ActiveProvider string                    `yaml:"active_provider"`
	Model          string                    `yaml:"model"`
	MaxTokens      int                       `yaml:"max_tokens"`
	SkillsDir      string                    `yaml:"skills_dir"`
	ActiveSkills   []string                  `yaml:"active_skills"`
	Skills         SkillsConfig              `yaml:"skills"`
	Memory         MemoryConfig              `yaml:"memory"`
	Server         ServerConfig              `yaml:"server"`
	MCP            MCPConfig                 `yaml:"mcp"`
	Hooks          map[string][]HookConfig   `yaml:"hooks"`
	Lifecycle      LifecycleConfig           `yaml:"lifecycle"`
	Sessions       SessionsConfig            `yaml:"sessions"`
	Sandbox        SandboxConfig             `yaml:"sandbox"`
}

type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

type CompactionConfig struct {
	Threshold float64 `yaml:"threshold"`
	Model     string  `yaml:"model"`
}

type MemoryConfig struct {
	Store      string           `yaml:"store"`
	Dir        string           `yaml:"dir"`
	Compaction CompactionConfig `yaml:"compaction"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type MCPServerConfig struct {
	Name      string            `yaml:"name"`
	Command   string            `yaml:"command,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Transport string            `yaml:"transport"`
	Timeout   time.Duration     `yaml:"timeout"`
	Env       map[string]string `yaml:"env,omitempty"`
}

type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Memory.Dir == "" {
		cfg.Memory.Dir = "./.agent/memory"
	}
	if cfg.Memory.Store == "" {
		cfg.Memory.Store = "file"
	}
	if cfg.Memory.Compaction.Threshold <= 0 {
		cfg.Memory.Compaction.Threshold = 0.8
	}
	if cfg.Skills.ProjectDir == "" {
		if cfg.SkillsDir != "" {
			cfg.Skills.ProjectDir = cfg.SkillsDir
		} else {
			cfg.Skills.ProjectDir = "./.agent/skills"
		}
	}
	if cfg.Skills.UserDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			cfg.Skills.UserDir = filepath.Join(home, ".agent", "skills")
		}
	}
	if cfg.Lifecycle.ShutdownTimeout <= 0 {
		cfg.Lifecycle.ShutdownTimeout = 30 * time.Second
	}
	if cfg.Sessions.MaxSessions <= 0 {
		cfg.Sessions.MaxSessions = 100
	}
	if cfg.Sessions.TTL <= 0 {
		cfg.Sessions.TTL = 30 * time.Minute
	}
	if cfg.Sessions.CleanupInterval <= 0 {
		cfg.Sessions.CleanupInterval = 1 * time.Minute
	}

	return &cfg, nil
}
