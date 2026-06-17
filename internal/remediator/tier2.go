package remediator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/llmjson"
	"github.com/teochenglim/sora/internal/types"
)

// PlanStep is one tool invocation in a Tier-2 remediation plan.
type PlanStep struct {
	Tool   string            `json:"tool"`
	Params map[string]string `json:"params"`
}

// Plan is the LLM's proposed remediation, with a confidence score that
// gates whether Tier 2 proceeds or defers to Tier 3 human escalation.
type Plan struct {
	Steps      []PlanStep `json:"steps"`
	Confidence float64    `json:"confidence"`
}

// Planner produces a remediation Plan for a classified alert.
type Planner interface {
	Plan(ctx context.Context, alert types.ClassifiedAlert) (Plan, error)
}

// httpPlanner calls an OpenAI-chat-completions-compatible endpoint to
// produce a Plan, reusing the same provider-agnostic approach as the
// classifier's AI client.
type httpPlanner struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	model       string
	temperature float64
}

// NewHTTPPlanner builds a Planner from AI config (same provider as classifier).
func NewHTTPPlanner(cfg config.AIConfig, baseURL string) Planner {
	return &httpPlanner{
		httpClient:  &http.Client{Timeout: cfg.Timeout},
		baseURL:     baseURL,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
	}
}

const tier2SystemPrompt = `You are SORA's remediation planner. Given an incident, propose at most 3 tool calls from {query_pod_status, query_logs, restart_service} to investigate/resolve it.
Respond with ONLY JSON: {"steps":[{"tool":"string","params":{"namespace":"...","pod":"...","service":"..."}}],"confidence":0.0}`

func (p *httpPlanner) Plan(ctx context.Context, alert types.ClassifiedAlert) (Plan, error) {
	userMsg := fmt.Sprintf("Alert: %s\nLevel: %s\nNamespace: %s\nPod: %s\nService: %s\nRoot cause hint: %s",
		alert.AlertName, alert.Level, alert.Namespace, alert.Pod, alert.Service, alert.RootCauseHint)

	body := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": tier2SystemPrompt},
			{"role": "user", "content": userMsg},
		},
		"temperature": p.temperature,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Plan{}, fmt.Errorf("marshalling planner request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Plan{}, fmt.Errorf("building planner request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Plan{}, fmt.Errorf("calling planner endpoint: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Plan{}, fmt.Errorf("reading planner response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Plan{}, fmt.Errorf("planner endpoint returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return Plan{}, fmt.Errorf("parsing planner response envelope: %w", err)
	}
	if len(envelope.Choices) == 0 {
		return Plan{}, fmt.Errorf("planner response contained no choices")
	}

	var plan Plan
	if err := json.Unmarshal([]byte(llmjson.Extract(envelope.Choices[0].Message.Content)), &plan); err != nil {
		return Plan{}, fmt.Errorf("parsing plan JSON: %w", err)
	}
	return plan, nil
}

// Tier2 is the LLM plan-and-execute remediation engine.
type Tier2 struct {
	planner      Planner
	executor     *Executor
	maxToolCalls int
	toolTimeout  time.Duration
	totalBudget  time.Duration
}

// NewTier2 creates a Tier2 engine.
func NewTier2(planner Planner, executor *Executor, maxToolCalls int, toolTimeout time.Duration) *Tier2 {
	return &Tier2{planner: planner, executor: executor, maxToolCalls: maxToolCalls, toolTimeout: toolTimeout, totalBudget: 90 * time.Second}
}

// Attempt produces a plan and, if confidence is high enough, executes it.
// Outcome.RequiresApproval is set when confidence is below 0.8, signalling
// the caller to fall through to Tier 3.
func (t *Tier2) Attempt(ctx context.Context, alert types.ClassifiedAlert) (Outcome, error) {
	budgetCtx, cancel := context.WithTimeout(ctx, t.totalBudget)
	defer cancel()

	plan, err := t.planner.Plan(budgetCtx, alert)
	if err != nil {
		return Outcome{}, fmt.Errorf("tier2 planning failed: %w", err)
	}
	if plan.Confidence < 0.8 {
		return Outcome{Handled: true, RequiresApproval: true, Action: types.ActionRecord{
			Tier: "tier2", Action: "escalate-low-confidence", Params: fmt.Sprintf("confidence=%.2f", plan.Confidence),
		}}, nil
	}

	steps := plan.Steps
	if len(steps) > t.maxToolCalls {
		steps = steps[:t.maxToolCalls]
	}

	var lastRecord types.ActionRecord
	for _, step := range steps {
		stepCtx, stepCancel := context.WithTimeout(budgetCtx, t.toolTimeout)
		started := time.Now()
		result, execErr := t.executor.Run(stepCtx, step.Tool, step.Params)
		stepCancel()

		record := types.ActionRecord{
			Tier: "tier2", Action: step.Tool, Params: fmt.Sprintf("%v", step.Params),
			Success: execErr == nil && result.Success, StartedAt: started, FinishedAt: time.Now(),
		}
		if execErr != nil {
			record.Error = execErr.Error()
		}
		lastRecord = record
	}
	return Outcome{Handled: true, Action: lastRecord}, nil
}
