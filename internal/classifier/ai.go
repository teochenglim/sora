package classifier

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

// AIClassifier talks to any OpenAI chat-completions-compatible endpoint
// (OpenAI, DeepSeek, Ollama, LiteLLM proxy in front of Anthropic, etc.),
// selected purely via base URL / model / API key configuration.
type AIClassifier interface {
	Classify(ctx context.Context, alert types.Alert, similar []types.Alert) (types.ClassifiedAlert, error)
}

type httpAIClassifier struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxRetries  int
}

// defaultBaseURL returns the conventional OpenAI-compatible base URL for
// well-known providers when the operator hasn't set one explicitly.
func defaultBaseURL(provider string) string {
	switch provider {
	case "openai":
		return "https://api.openai.com/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "ollama":
		return "http://localhost:11434/v1"
	default:
		// anthropic and any other provider are expected to be reached
		// through an OpenAI-compatible gateway (e.g. LiteLLM) configured
		// via LLM_BASE_URL.
		return "https://api.openai.com/v1"
	}
}

// NewAIClassifier builds an AIClassifier from AI config.
func NewAIClassifier(cfg config.AIConfig) AIClassifier {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL(cfg.Provider)
	}
	return &httpAIClassifier{
		httpClient:  &http.Client{Timeout: cfg.Timeout},
		baseURL:     baseURL,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxRetries:  cfg.MaxRetries,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

type aiOutput struct {
	Level              string   `json:"level"`
	BusinessLine       string   `json:"business_line"`
	RootCauseHint      string   `json:"root_cause_hint"`
	RecommendedActions []string `json:"recommended_actions"`
	Confidence         float64  `json:"confidence"`
}

func (c *httpAIClassifier) Classify(ctx context.Context, alert types.Alert, similar []types.Alert) (types.ClassifiedAlert, error) {
	reqBody := chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: BuildUserPrompt(alert, similar)},
		},
		Temperature: c.temperature,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return types.ClassifiedAlert{}, fmt.Errorf("marshalling AI request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff(attempt)):
			case <-ctx.Done():
				return types.ClassifiedAlert{}, ctx.Err()
			}
		}

		out, err := c.doRequest(ctx, payload)
		if err == nil {
			return toClassifiedAlert(alert, out), nil
		}
		lastErr = err
	}
	return types.ClassifiedAlert{}, fmt.Errorf("AI classification failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

func (c *httpAIClassifier) doRequest(ctx context.Context, payload []byte) (aiOutput, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return aiOutput{}, fmt.Errorf("building AI request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return aiOutput{}, fmt.Errorf("calling AI endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return aiOutput{}, fmt.Errorf("reading AI response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return aiOutput{}, fmt.Errorf("AI endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return aiOutput{}, fmt.Errorf("parsing AI response envelope: %w", err)
	}
	if len(cr.Choices) == 0 {
		return aiOutput{}, fmt.Errorf("AI response contained no choices")
	}

	var out aiOutput
	if err := json.Unmarshal([]byte(llmjson.Extract(cr.Choices[0].Message.Content)), &out); err != nil {
		return aiOutput{}, fmt.Errorf("parsing AI structured output: %w", err)
	}
	return out, nil
}

func toClassifiedAlert(alert types.Alert, out aiOutput) types.ClassifiedAlert {
	return types.ClassifiedAlert{
		Alert:         alert,
		Level:         out.Level,
		BusinessLine:  out.BusinessLine,
		RootCauseHint: out.RootCauseHint,
		Actions:       out.RecommendedActions,
		Confidence:    out.Confidence,
		ClassifiedBy:  types.ClassifiedByAI,
	}
}

func backoff(attempt int) time.Duration {
	return time.Duration(attempt) * 500 * time.Millisecond
}
