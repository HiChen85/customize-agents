package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type SandboxConfig struct {
	AllowedCommands []string `yaml:"allowed_commands"`
	BlockedCommands []string `yaml:"blocked_commands"`
	AllowedPaths    []string `yaml:"allowed_paths"`
	BlockedPaths    []string `yaml:"blocked_paths"`
	MaxOutputSize   int      `yaml:"max_output_size"`
}

type Sandbox struct {
	config SandboxConfig
	mu     sync.RWMutex
}

func NewSandbox(config SandboxConfig) *Sandbox {
	if config.MaxOutputSize <= 0 {
		config.MaxOutputSize = 102400
	}
	return &Sandbox{config: config}
}

func (s *Sandbox) Check(command string) error {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	segments := splitCommand(command)
	for _, seg := range segments {
		binary := extractBinary(seg)
		if err := s.checkBinary(binary, cfg); err != nil {
			return err
		}
		if err := s.checkPaths(seg, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sandbox) checkBinary(binary string, cfg SandboxConfig) error {
	for _, blocked := range cfg.BlockedCommands {
		if binary == blocked {
			return fmt.Errorf("sandbox: command '%s' is blocked", binary)
		}
	}
	if len(cfg.AllowedCommands) > 0 {
		allowed := false
		for _, a := range cfg.AllowedCommands {
			if binary == a {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("sandbox: command '%s' is not in allowed list", binary)
		}
	}
	return nil
}

func (s *Sandbox) checkPaths(segment string, cfg SandboxConfig) error {
	tokens := strings.Fields(segment)
	for _, token := range tokens {
		if !strings.HasPrefix(token, "/") && !strings.HasPrefix(token, "~/") {
			continue
		}
		for _, blocked := range cfg.BlockedPaths {
			if strings.HasPrefix(token, blocked) {
				return fmt.Errorf("sandbox: path '%s' is blocked", token)
			}
		}
		if len(cfg.AllowedPaths) > 0 {
			allowed := false
			for _, a := range cfg.AllowedPaths {
				if strings.HasPrefix(token, a) {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("sandbox: path '%s' is not in allowed paths", token)
			}
		}
	}
	return nil
}

func (s *Sandbox) TruncateOutput(output string) string {
	s.mu.RLock()
	maxSize := s.config.MaxOutputSize
	s.mu.RUnlock()

	if maxSize <= 0 || len(output) <= maxSize {
		return output
	}
	return output[:maxSize] + fmt.Sprintf("\n... [truncated, total %d bytes]", len(output))
}

func (s *Sandbox) UpdateConfig(config SandboxConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if config.MaxOutputSize <= 0 {
		config.MaxOutputSize = 102400
	}
	s.config = config
}

func (s *Sandbox) WrapExecTool(tool Tool) Tool {
	originalExecute := tool.Execute
	tool.Execute = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("sandbox: parse input: %w", err)
		}

		if err := s.Check(params.Command); err != nil {
			return "", err
		}

		output, err := originalExecute(ctx, input)
		if err != nil {
			return output, err
		}
		return s.TruncateOutput(output), nil
	}
	return tool
}

func splitCommand(cmd string) []string {
	for _, sep := range []string{"&&", "||", ";"} {
		if strings.Contains(cmd, sep) {
			parts := strings.Split(cmd, sep)
			segments := make([]string, 0, len(parts))
			for _, p := range parts {
				if trimmed := strings.TrimSpace(p); trimmed != "" {
					segments = append(segments, trimmed)
				}
			}
			return segments
		}
	}
	return []string{cmd}
}

func extractBinary(segment string) string {
	segment = strings.TrimSpace(segment)
	fields := strings.Fields(segment)
	if len(fields) == 0 {
		return ""
	}
	binary := fields[0]
	if idx := strings.LastIndex(binary, "/"); idx >= 0 {
		binary = binary[idx+1:]
	}
	return binary
}
