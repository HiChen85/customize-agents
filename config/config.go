package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Providers      map[string]ProviderConfig `yaml:"providers"`
	ActiveProvider string                    `yaml:"active_provider"`
	Model          string                    `yaml:"model"`
	MaxTokens      int                       `yaml:"max_tokens"`
	SkillsDir      string                    `yaml:"skills_dir"`
	ActiveSkills   []string                  `yaml:"active_skills"`
	Memory         MemoryConfig              `yaml:"memory"`
	Server         ServerConfig              `yaml:"server"`
	MCPServers     []MCPServerConfig         `yaml:"mcp_servers"`
}

type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

type MemoryConfig struct {
	Store string `yaml:"store"`
	Dir   string `yaml:"dir"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type MCPServerConfig struct {
	Name      string `yaml:"name"`
	Command   string `yaml:"command,omitempty"`
	URL       string `yaml:"url,omitempty"`
	Transport string `yaml:"transport"`
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
	if cfg.SkillsDir == "" {
		cfg.SkillsDir = "./skills"
	}

	return &cfg, nil
}
