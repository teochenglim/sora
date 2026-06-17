// Package notifier sends incident notifications to chat platforms.
// Adding a new IM channel only requires implementing this interface.
package notifier

import (
	"context"

	"github.com/teochenglim/sora/internal/types"
)

// Notifier delivers incident notifications to a single channel.
type Notifier interface {
	// Notify sends a standard alert/classification notification.
	Notify(ctx context.Context, incident types.Incident) error
	// Escalate sends a Tier-3 human-escalation message with
	// approve/reject/snooze actions attached.
	Escalate(ctx context.Context, incident types.Incident) error
	Channel() string
}

// WorkHours describes the window during which P2 notifications are sent.
type WorkHours struct {
	Start    string // "HH:MM"
	End      string // "HH:MM"
	Timezone string
	Days     []string // "Mon".."Sun"
}
