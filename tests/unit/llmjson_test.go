package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/teochenglim/sora/internal/llmjson"
)

func TestExtract_PlainJSONPassesThrough(t *testing.T) {
	assert.Equal(t, `{"level":"P0"}`, llmjson.Extract(`{"level":"P0"}`))
}

func TestExtract_StripsJSONFence(t *testing.T) {
	in := "```json\n{\"level\":\"P0\"}\n```"
	assert.Equal(t, `{"level":"P0"}`, llmjson.Extract(in))
}

func TestExtract_StripsPlainFence(t *testing.T) {
	in := "```\n{\"level\":\"P0\"}\n```"
	assert.Equal(t, `{"level":"P0"}`, llmjson.Extract(in))
}

func TestExtract_TrimsSurroundingWhitespace(t *testing.T) {
	assert.Equal(t, `{"level":"P0"}`, llmjson.Extract("  \n{\"level\":\"P0\"}\n  "))
}
