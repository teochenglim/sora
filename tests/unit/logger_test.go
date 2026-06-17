package unit

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/teochenglim/sora/pkg/logger"
)

func TestLogger_PrefixesMessages(t *testing.T) {
	l := logger.New("info")
	var buf bytes.Buffer
	l.SetOutput(&buf)

	l.Info("hello")
	assert.Contains(t, buf.String(), "[SORA] hello")
}

func TestLogger_InvalidLevelFallsBackToInfo(t *testing.T) {
	l := logger.New("not-a-level")
	assert.Equal(t, "info", l.GetLevel().String())
}
