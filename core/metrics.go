package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type ToolMetrics struct {
	TotalCalls    int64         `json:"total_calls"`
	Successes     int64         `json:"successes"`
	Failures      int64         `json:"failures"`
	TotalDuration time.Duration `json:"total_duration"`
}

type LLMMetrics struct {
	TotalCalls     int64         `json:"total_calls"`
	TotalDuration  time.Duration `json:"total_duration"`
	TotalTokensIn  int64         `json:"total_tokens_in"`
	TotalTokensOut int64         `json:"total_tokens_out"`
}

type MetricsSnapshot struct {
	Tools     map[string]*ToolMetrics `json:"tools"`
	LLM       LLMMetrics              `json:"llm"`
	StartTime time.Time               `json:"start_time"`
	Uptime    time.Duration           `json:"uptime"`
}

type MetricsCollector struct {
	mu        sync.RWMutex
	tools     map[string]*ToolMetrics
	llm       LLMMetrics
	startTime time.Time
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		tools:     make(map[string]*ToolMetrics),
		startTime: time.Now(),
	}
}

func (m *MetricsCollector) RecordToolCall(name string, duration time.Duration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tm, ok := m.tools[name]
	if !ok {
		tm = &ToolMetrics{}
		m.tools[name] = tm
	}

	tm.TotalCalls++
	tm.TotalDuration += duration
	if err != nil {
		tm.Failures++
	} else {
		tm.Successes++
	}
}

func (m *MetricsCollector) RecordLLMCall(duration time.Duration, inputTokens, outputTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.llm.TotalCalls++
	m.llm.TotalDuration += duration
	m.llm.TotalTokensIn += int64(inputTokens)
	m.llm.TotalTokensOut += int64(outputTokens)
}

func (m *MetricsCollector) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tools := make(map[string]*ToolMetrics, len(m.tools))
	for name, tm := range m.tools {
		cp := *tm
		tools[name] = &cp
	}

	return MetricsSnapshot{
		Tools:     tools,
		LLM:       m.llm,
		StartTime: m.startTime,
		Uptime:    time.Since(m.startTime),
	}
}

func (m *MetricsCollector) PrometheusFormat() string {
	snap := m.Snapshot()
	var sb strings.Builder

	sb.WriteString("# HELP agent_tool_calls_total Total tool calls by tool and status\n")
	sb.WriteString("# TYPE agent_tool_calls_total counter\n")
	for name, tm := range snap.Tools {
		fmt.Fprintf(&sb, "agent_tool_calls_total{tool=\"%s\",status=\"success\"} %d\n", name, tm.Successes)
		fmt.Fprintf(&sb, "agent_tool_calls_total{tool=\"%s\",status=\"error\"} %d\n", name, tm.Failures)
	}

	sb.WriteString("# HELP agent_tool_duration_seconds_total Total duration of tool calls in seconds\n")
	sb.WriteString("# TYPE agent_tool_duration_seconds_total counter\n")
	for name, tm := range snap.Tools {
		fmt.Fprintf(&sb, "agent_tool_duration_seconds_total{tool=\"%s\"} %.3f\n", name, tm.TotalDuration.Seconds())
	}

	sb.WriteString("# HELP agent_llm_calls_total Total LLM API calls\n")
	sb.WriteString("# TYPE agent_llm_calls_total counter\n")
	fmt.Fprintf(&sb, "agent_llm_calls_total %d\n", snap.LLM.TotalCalls)

	sb.WriteString("# HELP agent_llm_duration_seconds_total Total LLM call duration in seconds\n")
	sb.WriteString("# TYPE agent_llm_duration_seconds_total counter\n")
	fmt.Fprintf(&sb, "agent_llm_duration_seconds_total %.3f\n", snap.LLM.TotalDuration.Seconds())

	sb.WriteString("# HELP agent_llm_tokens_total Total tokens consumed\n")
	sb.WriteString("# TYPE agent_llm_tokens_total counter\n")
	fmt.Fprintf(&sb, "agent_llm_tokens_total{direction=\"input\"} %d\n", snap.LLM.TotalTokensIn)
	fmt.Fprintf(&sb, "agent_llm_tokens_total{direction=\"output\"} %d\n", snap.LLM.TotalTokensOut)

	return sb.String()
}

func RegisterMetricsHooks(registry *HookRegistry, collector *MetricsCollector) {
	registry.Register(AfterToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		collector.RecordToolCall(p.ToolName, p.Duration, p.Error)
		return nil
	}))
	registry.Register(AfterLLMCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		var in, out int
		if p.Response != nil {
			in = p.Response.Usage.InputTokens
			out = p.Response.Usage.OutputTokens
		}
		collector.RecordLLMCall(p.Duration, in, out)
		return nil
	}))
}
