package tools

import (
	"context"
	"fmt"
	"sync"
)

// MockTool is a configurable stand-in for a real Tool, used by --mode=demo
// and by tests. It records every invocation it receives.
type MockTool struct {
	mu          sync.Mutex
	name        string
	output      string
	success     bool
	err         error
	invocations []map[string]string
}

// NewMockTool creates a MockTool that always returns the given result.
func NewMockTool(name, output string, success bool) *MockTool {
	return &MockTool{name: name, output: output, success: success}
}

func (t *MockTool) Name() string { return t.name }

// SetError makes the next Execute calls return err instead of a result.
func (t *MockTool) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

func (t *MockTool) Execute(_ context.Context, params map[string]string) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.invocations = append(t.invocations, params)
	if t.err != nil {
		return Result{}, t.err
	}
	return Result{Output: t.output, Success: t.success}, nil
}

// Invocations returns a copy of all params this tool was called with.
func (t *MockTool) Invocations() []map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]map[string]string, len(t.invocations))
	copy(out, t.invocations)
	return out
}

// NewDemoRegistry returns a Registry of mock tools that behave plausibly
// for --mode=demo, with no real Kubernetes cluster required.
func NewDemoRegistry() *Registry {
	return NewRegistry(
		NewMockTool(ToolQueryPodStatus, "phase=Running restarts=2", true),
		NewMockTool(ToolQueryLogs, "demo log line: connection reset by peer", true),
		NewMockTool(ToolRestartService, "restart triggered (demo)", true),
	)
}

// DemoAlertGenerator fires a sample alert every interval; used only in
// --mode=demo so the pipeline has something to react to with zero
// external dependencies.
type DemoAlertGenerator struct {
	samples []DemoAlert
	idx     int
	mu      sync.Mutex
}

// DemoAlert is a canned alert payload for demo mode.
type DemoAlert struct {
	AlertName, Severity, Namespace, Pod, Service, Instance string
}

// NewDemoAlertGenerator returns a generator cycling through a small set
// of representative sample alerts.
func NewDemoAlertGenerator() *DemoAlertGenerator {
	return &DemoAlertGenerator{samples: []DemoAlert{
		{AlertName: "CrashLoopBackOff", Severity: "critical", Namespace: "default", Pod: "worker-service-7d8f9-abcde", Service: "worker-service", Instance: "10.0.0.5"},
		{AlertName: "HighCPUUsage", Severity: "warning", Namespace: "default", Pod: "batch-processor-1", Service: "batch-processor", Instance: "10.0.0.6"},
		{AlertName: "OOMKilled", Severity: "critical", Namespace: "default", Pod: "payments-api-2", Service: "payments-api", Instance: "10.0.0.7"},
	}}
}

// Next returns the next sample alert in rotation.
func (g *DemoAlertGenerator) Next() DemoAlert {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.samples[g.idx%len(g.samples)]
	g.idx++
	return a
}

// String implements fmt.Stringer for log lines.
func (a DemoAlert) String() string {
	return fmt.Sprintf("%s/%s in %s", a.AlertName, a.Pod, a.Namespace)
}
