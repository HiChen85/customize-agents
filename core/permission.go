package core

import "encoding/json"

type PermissionConfig struct {
	AutoApprove []string
	PromptFunc  func(toolName string, input json.RawMessage) bool
}

type PermissionHandler struct {
	config      PermissionConfig
	autoApprove map[string]bool
}

func NewPermissionHandler(config PermissionConfig) *PermissionHandler {
	approved := make(map[string]bool, len(config.AutoApprove))
	for _, name := range config.AutoApprove {
		approved[name] = true
	}
	return &PermissionHandler{config: config, autoApprove: approved}
}

func (h *PermissionHandler) CheckPermission(toolName string, input json.RawMessage) bool {
	if h.autoApprove[toolName] {
		return true
	}
	if h.config.PromptFunc != nil {
		return h.config.PromptFunc(toolName, input)
	}
	return false
}
