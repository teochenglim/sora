package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teochenglim/sora/internal/classifier"
	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

func chatCompletionStub(t *testing.T, content string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + jsonQuote(content) + `}}]}`))
	}))
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAIClassifier_ParsesStructuredOutput(t *testing.T) {
	srv := chatCompletionStub(t, `{"level":"P0","business_line":"payments","root_cause_hint":"db down","recommended_actions":["restart_service"],"confidence":0.95}`, http.StatusOK)
	defer srv.Close()

	ai := classifier.NewAIClassifier(config.AIConfig{BaseURL: srv.URL, Model: "test-model", Timeout: 2 * time.Second})
	result, err := ai.Classify(context.Background(), types.Alert{AlertName: "PaymentsDown"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "P0", result.Level)
	assert.Equal(t, "payments", result.BusinessLine)
	assert.Equal(t, 0.95, result.Confidence)
	assert.Equal(t, types.ClassifiedByAI, result.ClassifiedBy)
}

func TestAIClassifier_RetriesThenFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ai := classifier.NewAIClassifier(config.AIConfig{BaseURL: srv.URL, Model: "test-model", Timeout: 2 * time.Second, MaxRetries: 1})
	_, err := ai.Classify(context.Background(), types.Alert{AlertName: "X"}, nil)
	assert.Error(t, err)
}
