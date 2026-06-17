// Package cache holds a short-lived, in-memory context cache used to
// inject recent similar alerts into AI classification prompts.
package cache

import (
	"strings"
	"sync"
	"time"

	"github.com/teochenglim/sora/internal/types"
)

const maxSimilarPerKey = 5

type entry struct {
	alert     types.Alert
	expiresAt time.Time
}

// ContextCache stores the most recent alerts per (alertname, namespace) key
// for ttl, so the classifier can find similar recent incidents.
type ContextCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string][]entry
}

// New creates a ContextCache with the given TTL (default 15 minutes if 0).
func New(ttl time.Duration) *ContextCache {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &ContextCache{ttl: ttl, entries: make(map[string][]entry)}
}

func key(alertName, namespace string) string {
	return strings.ToLower(alertName) + "|" + strings.ToLower(namespace)
}

// Store records a as having occurred, for future similarity lookups.
func (c *ContextCache) Store(a types.Alert) {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := key(a.AlertName, a.Namespace)
	now := time.Now()
	c.purgeExpiredLocked(k, now)

	list := c.entries[k]
	list = append(list, entry{alert: a, expiresAt: now.Add(c.ttl)})
	if len(list) > maxSimilarPerKey {
		list = list[len(list)-maxSimilarPerKey:]
	}
	c.entries[k] = list
}

// Similar returns up to maxSimilarPerKey recent alerts sharing the same
// alertname and namespace.
func (c *ContextCache) Similar(alertName, namespace string) []types.Alert {
	c.mu.Lock()
	defer c.mu.Unlock()

	k := key(alertName, namespace)
	now := time.Now()
	c.purgeExpiredLocked(k, now)

	list := c.entries[k]
	out := make([]types.Alert, 0, len(list))
	for _, e := range list {
		out = append(out, e.alert)
	}
	return out
}

func (c *ContextCache) purgeExpiredLocked(k string, now time.Time) {
	list := c.entries[k]
	if len(list) == 0 {
		return
	}
	fresh := list[:0]
	for _, e := range list {
		if now.Before(e.expiresAt) {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) == 0 {
		delete(c.entries, k)
		return
	}
	c.entries[k] = fresh
}
