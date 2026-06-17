package remediator

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"

	"github.com/teochenglim/sora/internal/config"
)

// PromotionThreshold is the number of successful resolutions a pattern
// needs before it is promoted from learned history to a Tier-1 rule.
const PromotionThreshold = 5

// Pattern is a learned (alertname, action) pair with its success count.
type Pattern struct {
	ID            int64
	AlertName     string
	Action        string
	SuccessCount  int
	Promoted      bool
}

// LearningStore persists successful Tier-2/Tier-3 resolutions and
// periodically promotes high-confidence ones into Tier-1 rules.yaml.
type LearningStore struct {
	db *sql.DB
}

// NewLearningStore opens (creating if needed) the SQLite database at path.
func NewLearningStore(path string) (*LearningStore, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("opening learning store %s: %w", path, err)
	}
	schema := `CREATE TABLE IF NOT EXISTS patterns (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		alertname TEXT NOT NULL,
		action TEXT NOT NULL,
		success_count INTEGER NOT NULL DEFAULT 0,
		promoted INTEGER NOT NULL DEFAULT 0,
		UNIQUE(alertname, action)
	)`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("creating patterns table: %w", err)
	}
	return &LearningStore{db: db}, nil
}

// RecordSuccess increments the success counter for (alertname, action),
// inserting a new row on first occurrence.
func (s *LearningStore) RecordSuccess(ctx context.Context, alertName, action string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO patterns (alertname, action, success_count) VALUES (?, ?, 1)
		ON CONFLICT(alertname, action) DO UPDATE SET success_count = success_count + 1
	`, alertName, action)
	if err != nil {
		return fmt.Errorf("recording success for %s/%s: %w", alertName, action, err)
	}
	return nil
}

// Patterns returns all learned patterns, for the /admin/patterns endpoint.
func (s *LearningStore) Patterns(ctx context.Context) ([]Pattern, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, alertname, action, success_count, promoted FROM patterns`)
	if err != nil {
		return nil, fmt.Errorf("querying patterns: %w", err)
	}
	defer rows.Close()

	var out []Pattern
	for rows.Next() {
		var p Pattern
		var promoted int
		if err := rows.Scan(&p.ID, &p.AlertName, &p.Action, &p.SuccessCount, &promoted); err != nil {
			return nil, fmt.Errorf("scanning pattern row: %w", err)
		}
		p.Promoted = promoted != 0
		out = append(out, p)
	}
	return out, nil
}

// PromoteEligible finds unpromoted patterns at or above PromotionThreshold,
// appends a Tier-1 remediation rule for each to rulesPath, and marks them
// promoted. Intended to run on an hourly ticker.
func (s *LearningStore) PromoteEligible(ctx context.Context, rulesPath string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, alertname, action FROM patterns WHERE success_count >= ? AND promoted = 0
	`, PromotionThreshold)
	if err != nil {
		return 0, fmt.Errorf("querying eligible patterns: %w", err)
	}

	type candidate struct {
		id        int64
		alertName string
		action    string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.alertName, &c.action); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning candidate row: %w", err)
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if len(candidates) == 0 {
		return 0, nil
	}

	data, err := os.ReadFile(rulesPath)
	if err != nil {
		return 0, fmt.Errorf("reading rules file %s: %w", rulesPath, err)
	}
	var rf config.RulesFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return 0, fmt.Errorf("parsing rules file %s: %w", rulesPath, err)
	}

	for _, c := range candidates {
		rule := config.RemediationRule{
			Name:            fmt.Sprintf("learned-%s-%s", c.alertName, c.action),
			Action:          c.action,
			RequireApproval: false,
			MaxPerHour:      5,
		}
		rule.Match.AlertNameRegex = c.alertName
		rf.RemediationRules = append(rf.RemediationRules, rule)
	}

	out, err := yaml.Marshal(rf)
	if err != nil {
		return 0, fmt.Errorf("marshalling promoted rules: %w", err)
	}
	if err := os.WriteFile(rulesPath, out, 0o644); err != nil {
		return 0, fmt.Errorf("writing rules file %s: %w", rulesPath, err)
	}

	for _, c := range candidates {
		if _, err := s.db.ExecContext(ctx, `UPDATE patterns SET promoted = 1 WHERE id = ?`, c.id); err != nil {
			return 0, fmt.Errorf("marking pattern %d promoted: %w", c.id, err)
		}
	}
	return len(candidates), nil
}

func (s *LearningStore) Close() error { return s.db.Close() }
