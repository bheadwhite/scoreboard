package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bheadwhite/scoreboard/backend/internal/auth"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

// How often a comment is sent down an idle stream.
//
// Without traffic, an intermediary — Cloud Run's front end, a phone's carrier
// proxy, a corporate box — will eventually decide the connection is dead and
// close it. A comment line is ignored by EventSource and costs a few bytes.
const heartbeat = 25 * time.Second

// A stream is closed a little before Cloud Run's request timeout so the client
// reconnects on its own schedule rather than being cut off mid-event. The
// browser reconnects automatically either way; this just makes it tidy.
const maxStreamLife = 4 * time.Minute

// Events streams changes for one family to a browser over Server-Sent Events.
//
// SSE rather than WebSockets because every write in this app is already an
// ordinary RPC — nothing needs a client-to-server channel. That buys plain
// HTTP (so the session cookie and the dev proxy just work) and automatic
// reconnection from EventSource, which is the fiddly part of a socket.
//
// Events carry no data. A client reacts by dropping the matching cache entry
// and re-reading through the normal API, so there is one code path producing
// data and no way for a pushed copy to disagree with a fetched one.
func Events(st *store.Store, hub *events.Hub, authn *auth.Auth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := authn.IdentifyRequest(r)
		if !ok {
			http.Error(w, "sign in to continue", http.StatusUnauthorized)
			return
		}

		familyID := r.URL.Query().Get("familyId")
		if familyID == "" {
			http.Error(w, "familyId is required", http.StatusBadRequest)
			return
		}

		// Same rule as every other family-scoped read: you have to be in it.
		if _, err := st.MemberRole(r.Context(), familyID, id.ID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, "could not check membership", http.StatusInternalServerError)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		// Tells any nginx-shaped intermediary not to buffer the response, which
		// would hold events until the connection closed and defeat the point.
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		ch, cancel := hub.Subscribe(familyID)
		defer cancel()

		// An opening comment so the browser sees bytes immediately and treats
		// the connection as established.
		fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		deadline := time.NewTimer(maxStreamLife)
		defer deadline.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return

			case <-deadline.C:
				return

			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()

			case e, open := <-ch:
				if !open {
					return
				}
				payload, err := json.Marshal(e)
				if err != nil {
					log.Printf("events: marshal: %v", err)
					continue
				}
				if _, err := fmt.Fprintf(w, "event: change\ndata: %s\n\n", payload); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

// publish is how a handler announces that something changed. Safe with a nil
// hub so tests and any future command-line tooling can build services without
// one.
func (b base) publish(familyID string, kind events.Kind, matchID string) {
	if b.hub == nil || familyID == "" {
		return
	}
	b.hub.Publish(familyID, events.Event{Kind: kind, MatchID: matchID})
}
