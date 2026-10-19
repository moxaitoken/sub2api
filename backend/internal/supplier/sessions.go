package supplier

import (
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// sessionOwners binds upstream OAuth session IDs to the supplier that started them, so one
// supplier can never complete another's authorization. Like the upstream session store it
// is process-local, which matches how the admin OAuth flow already behaves.
type sessionOwners struct {
	mu      sync.Mutex
	entries map[string]sessionOwner
}

type sessionOwner struct {
	supplierID int64
	mode       string
	createdAt  time.Time
}

func newSessionOwners() *sessionOwners {
	return &sessionOwners{entries: make(map[string]sessionOwner)}
}

func (o *sessionOwners) put(sessionID string, supplierID int64, mode string, now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evictExpired(now)
	o.entries[sessionID] = sessionOwner{supplierID: supplierID, mode: mode, createdAt: now}
}

// lookup returns the auth mode of a live session owned by supplierID.
func (o *sessionOwners) lookup(sessionID string, supplierID int64, now time.Time) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	entry, ok := o.entries[sessionID]
	if !ok || entry.supplierID != supplierID || now.Sub(entry.createdAt) > oauth.SessionTTL {
		return "", false
	}
	return entry.mode, true
}

func (o *sessionOwners) drop(sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.entries, sessionID)
}

func (o *sessionOwners) evictExpired(now time.Time) {
	for id, entry := range o.entries {
		if now.Sub(entry.createdAt) > oauth.SessionTTL {
			delete(o.entries, id)
		}
	}
}
