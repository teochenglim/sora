package unit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/teochenglim/sora/internal/notifier"
)

func TestInWorkHours_WithinWindow(t *testing.T) {
	wh := notifier.WorkHours{Start: "09:00", End: "18:00", Timezone: "UTC", Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}}
	// Wednesday 2026-06-17 12:00 UTC
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	assert.True(t, notifier.InWorkHours(wh, now))
}

func TestInWorkHours_OutsideWindow(t *testing.T) {
	wh := notifier.WorkHours{Start: "09:00", End: "18:00", Timezone: "UTC", Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}}
	now := time.Date(2026, 6, 17, 22, 0, 0, 0, time.UTC)
	assert.False(t, notifier.InWorkHours(wh, now))
}

func TestInWorkHours_WrongDay(t *testing.T) {
	wh := notifier.WorkHours{Start: "09:00", End: "18:00", Timezone: "UTC", Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}}
	// 2026-06-20 is a Saturday
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	assert.False(t, notifier.InWorkHours(wh, now))
}

func TestInWorkHours_NoWindowConfiguredDefaultsToAlwaysOn(t *testing.T) {
	wh := notifier.WorkHours{}
	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	assert.True(t, notifier.InWorkHours(wh, now))
}
