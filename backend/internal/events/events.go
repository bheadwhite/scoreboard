// Package events is a tiny in-process publish/subscribe hub, used to tell a
// family's open browsers that something changed.
//
// In process, deliberately. A broker exists to coordinate across instances, and
// this service runs one: a single container holds every connection a family
// will ever open, so the "broker" is a map and a mutex. If that ever stops
// being true, this is the seam to swap for Redis or Valkey — nothing outside
// this package knows how delivery works.
//
// Events carry no payload beyond what changed. Clients react by invalidating
// the matching cache entry and re-reading through the normal API, so there is
// exactly one code path that produces data and no way for a pushed copy to
// disagree with a fetched one.
package events

import (
	"sync"
)

// Kind names the sort of thing that changed. Clients use it to decide which
// caches to drop.
type Kind string

const (
	// KindMatch covers a match being created, joined, declined, started,
	// claimed, conceded, disputed, resolved or cancelled.
	KindMatch Kind = "match"
	// KindFamily covers membership: somebody joined, left, or was removed.
	KindFamily Kind = "family"
	// KindGames covers a family adding a game of its own.
	KindGames Kind = "games"
)

// Event is what a subscriber receives.
type Event struct {
	Kind Kind `json:"kind"`
	// The match this concerns, when the kind is KindMatch. Lets a client
	// watching one match ignore noise from every other game in the family.
	MatchID string `json:"matchId,omitempty"`
}

// How many events a slow subscriber may fall behind before it is dropped.
// Small on purpose: a browser that cannot keep up with a family's activity is
// wedged, and disconnecting it makes EventSource reconnect and re-read, which
// is self-healing. Blocking the publisher instead would stall a request.
const bufferSize = 16

type subscriber struct {
	ch chan Event
}

// Hub fans events out to the browsers watching each family.
type Hub struct {
	mu       sync.RWMutex
	families map[string]map[*subscriber]struct{}
}

func NewHub() *Hub {
	return &Hub{families: make(map[string]map[*subscriber]struct{})}
}

// Subscribe returns a channel of events for one family, and a function that
// stops the subscription. The caller must always call it, or the subscriber
// leaks for the lifetime of the process.
func (h *Hub) Subscribe(familyID string) (<-chan Event, func()) {
	sub := &subscriber{ch: make(chan Event, bufferSize)}

	h.mu.Lock()
	if h.families[familyID] == nil {
		h.families[familyID] = make(map[*subscriber]struct{})
	}
	h.families[familyID][sub] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if subs, ok := h.families[familyID]; ok {
				delete(subs, sub)
				if len(subs) == 0 {
					delete(h.families, familyID)
				}
			}
			h.mu.Unlock()
			close(sub.ch)
		})
	}
	return sub.ch, cancel
}

// Publish delivers an event to everyone watching a family.
//
// Never blocks. A subscriber whose buffer is full is skipped rather than
// waited on — the request doing the publishing is somebody's tap on a button,
// and it must not be held up by a browser that stopped reading.
func (h *Hub) Publish(familyID string, e Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.families[familyID] {
		select {
		case sub.ch <- e:
		default:
			// Dropped. The client re-reads on reconnect, so a missed event
			// costs freshness for a moment, never correctness.
		}
	}
}

// Watchers reports how many browsers are watching a family. For diagnostics.
func (h *Hub) Watchers(familyID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.families[familyID])
}
