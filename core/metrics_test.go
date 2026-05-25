package core

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMetricsCollector_RecordToolCall_Success(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordToolCall("read_file", 100*time.Millisecond, nil)

	snap := mc.Snapshot()
	tool := snap.Tools["read_file"]
	if tool == nil {
		t.Fatal("expected read_file metrics")
	}
	if tool.TotalCalls != 1 {
		t.Errorf("expected 1 call, got %d", tool.TotalCalls)
	}
	if tool.Successes != 1 {
		t.Errorf("expected 1 success, got %d", tool.Successes)
	}
}

func TestMetricsCollector_RecordToolCall_Failure(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordToolCall("exec", 50*time.Millisecond, fmt.Errorf("timeout"))

	snap := mc.Snapshot()
	tool := snap.Tools["exec"]
	if tool.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", tool.Failures)
	}
}

func TestMetricsCollector_RecordLLMCall(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordLLMCall(200*time.Millisecond, 100, 50)
	mc.RecordLLMCall(300*time.Millisecond, 200, 80)

	snap := mc.Snapshot()
	if snap.LLM.TotalCalls != 2 {
		t.Errorf("expected 2 LLM calls, got %d", snap.LLM.TotalCalls)
	}
	if snap.LLM.TotalTokensIn != 300 {
		t.Errorf("expected 300 input tokens, got %d", snap.LLM.TotalTokensIn)
	}
	if snap.LLM.TotalTokensOut != 130 {
		t.Errorf("expected 130 output tokens, got %d", snap.LLM.TotalTokensOut)
	}
}

func TestMetricsCollector_PrometheusFormat(t *testing.T) {
	mc := NewMetricsCollector()
	mc.RecordToolCall("exec", 1*time.Second, nil)
	mc.RecordToolCall("exec", 2*time.Second, fmt.Errorf("fail"))
	mc.RecordLLMCall(500*time.Millisecond, 100, 50)

	output := mc.PrometheusFormat()

	if !strings.Contains(output, `agent_tool_calls_total{tool="exec",status="success"} 1`) {
		t.Errorf("missing exec success metric in:\n%s", output)
	}
	if !strings.Contains(output, `agent_tool_calls_total{tool="exec",status="error"} 1`) {
		t.Errorf("missing exec error metric in:\n%s", output)
	}
	if !strings.Contains(output, `agent_llm_calls_total 1`) {
		t.Errorf("missing llm calls metric in:\n%s", output)
	}
	if !strings.Contains(output, `agent_llm_tokens_total{direction="input"} 100`) {
		t.Errorf("missing input tokens metric in:\n%s", output)
	}
}

func TestMetricsCollector_Snapshot_ThreadSafe(t *testing.T) {
	mc := NewMetricsCollector()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			mc.RecordToolCall("tool", 1*time.Millisecond, nil)
		}
		close(done)
	}()

	for i := 0; i < 50; i++ {
		mc.Snapshot()
	}
	<-done

	snap := mc.Snapshot()
	if snap.Tools["tool"].TotalCalls != 100 {
		t.Errorf("expected 100, got %d", snap.Tools["tool"].TotalCalls)
	}
}
