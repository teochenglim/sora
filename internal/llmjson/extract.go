// Package llmjson sanitizes raw LLM chat-completion content before
// json.Unmarshal. Many models — especially local reasoning models like
// Ollama's — wrap structured output in a markdown code fence even when
// explicitly instructed to respond with "ONLY JSON".
package llmjson

import "strings"

// Extract strips a leading/trailing ```json or ``` fence (and any
// surrounding whitespace) from s, returning the content most likely to be
// valid JSON. If no fence is present, s is returned trimmed as-is.
func Extract(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
