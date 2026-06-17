// Package tools implements the remediation actions the Tier-1 rule engine
// and Tier-2 LLM planner can invoke against a Kubernetes cluster.
package tools

import "context"

// Tool is a single named, idempotent remediation/inspection action.
type Tool interface {
	Name() string
	Execute(ctx context.Context, params map[string]string) (Result, error)
}

// Result is the outcome of running a Tool.
type Result struct {
	Output  string `json:"output"`
	Success bool   `json:"success"`
}

// Registry resolves tool names to implementations.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds a Registry from the given tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		r.tools[t.Name()] = t
	}
	return r
}

// Get returns the tool registered under name, if any.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the set of registered tool names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

const (
	ToolQueryPodStatus  = "query_pod_status"
	ToolQueryLogs       = "query_logs"
	ToolRestartService  = "restart_service"
)
