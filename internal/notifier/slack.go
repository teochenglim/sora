package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/teochenglim/sora/internal/config"
	"github.com/teochenglim/sora/internal/types"
)

// SlackNotifier posts incident notifications to a Slack incoming webhook
// using Block Kit formatting.
type SlackNotifier struct {
	httpClient *http.Client
	webhookURL string
	owners     map[string]config.BusinessOwner
	workHours  WorkHours
}

// NewSlackNotifier creates a SlackNotifier.
func NewSlackNotifier(webhookURL string, owners []config.BusinessOwner, wh WorkHours) *SlackNotifier {
	idx := make(map[string]config.BusinessOwner, len(owners))
	for _, o := range owners {
		idx[o.Name] = o
	}
	return &SlackNotifier{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		webhookURL: webhookURL,
		owners:     idx,
		workHours:  wh,
	}
}

func (s *SlackNotifier) Channel() string { return "slack" }

func (s *SlackNotifier) Notify(ctx context.Context, incident types.Incident) error {
	level := incident.Alert.Level
	if level == types.LevelP2 && !InWorkHours(s.workHours, time.Now()) {
		return nil // P2 only sent during work hours
	}

	mention := ""
	switch level {
	case types.LevelP0:
		mention = "<!channel> "
	case types.LevelP1:
		mention = "<!here> "
	case types.LevelP2:
		if owner, ok := s.owners[incident.Alert.BusinessLine]; ok && owner.SlackID != "" {
			mention = fmt.Sprintf("<@%s> ", owner.SlackID)
		}
	}

	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: fmt.Sprintf("%s %s", level, incident.Alert.AlertName)}},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: fmt.Sprintf(
			"%sIncident `%s`\n*Service:* %s\n*Namespace:* %s\n*Business line:* %s\n*Root cause:* %s",
			mention, incident.ID, incident.Alert.Service, incident.Alert.Namespace, incident.Alert.BusinessLine, incident.Alert.RootCauseHint)}},
	}
	return s.post(ctx, slackMessage{Blocks: blocks})
}

func (s *SlackNotifier) Escalate(ctx context.Context, incident types.Incident) error {
	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: fmt.Sprintf("Escalation: %s %s", incident.Alert.Level, incident.Alert.AlertName)}},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: fmt.Sprintf(
			"Incident `%s` needs human approval.\n*Service:* %s\n*Recommended actions:* %v",
			incident.ID, incident.Alert.Service, incident.Alert.Actions)}},
		{
			Type: "actions",
			Elements: []slackElement{
				{Type: "button", Text: &slackText{Type: "plain_text", Text: "Approve"}, ActionID: "approve", Value: incident.ID, Style: "primary"},
				{Type: "button", Text: &slackText{Type: "plain_text", Text: "Reject"}, ActionID: "reject", Value: incident.ID, Style: "danger"},
				{Type: "button", Text: &slackText{Type: "plain_text", Text: "Snooze"}, ActionID: "snooze", Value: incident.ID},
			},
		},
	}
	return s.post(ctx, slackMessage{Blocks: blocks})
}

func (s *SlackNotifier) post(ctx context.Context, msg slackMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshalling slack message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting to slack: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return nil
}

type slackMessage struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string         `json:"type"`
	Text     *slackText     `json:"text,omitempty"`
	Elements []slackElement `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackElement struct {
	Type     string     `json:"type"`
	Text     *slackText `json:"text,omitempty"`
	ActionID string     `json:"action_id,omitempty"`
	Value    string     `json:"value,omitempty"`
	Style    string     `json:"style,omitempty"`
}
