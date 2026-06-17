// Package logger provides the structured JSON logger used across SORA.
package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

// New returns a logrus.Logger configured for JSON output with the
// "[SORA]" prefix applied to every message.
func New(level string) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(os.Stdout)
	l.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
	})

	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		lvl = logrus.InfoLevel
	}
	l.SetLevel(lvl)
	l.AddHook(&prefixHook{prefix: "[SORA] "})
	return l
}

type prefixHook struct {
	prefix string
}

func (h *prefixHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *prefixHook) Fire(e *logrus.Entry) error {
	if len(e.Message) == 0 || e.Message[:1] != "[" {
		e.Message = h.prefix + e.Message
	}
	return nil
}
