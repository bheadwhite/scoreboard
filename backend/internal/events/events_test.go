package events

import (
	"sync"
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("expected an event, got nothing")
		return Event{}
	}
}

func TestPublishReachesEverySubscriber(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe("fam")
	b, cancelB := h.Subscribe("fam")
	defer cancelA()
	defer cancelB()

	h.Publish("fam", Event{Kind: KindMatch, MatchID: "m1"})

	if got := recv(t, a); got.MatchID != "m1" {
		t.Errorf("a: got %+v", got)
	}
	if got := recv(t, b); got.MatchID != "m1" {
		t.Errorf("b: got %+v", got)
	}
}

func TestEventsDoNotCrossFamilies(t *testing.T) {
	h := NewHub()
	mine, cancel := h.Subscribe("mine")
	defer cancel()

	h.Publish("theirs", Event{Kind: KindMatch, MatchID: "m1"})

	select {
	case e := <-mine:
		t.Fatalf("received another family's event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCancelStopsDelivery(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe("fam")
	cancel()

	h.Publish("fam", Event{Kind: KindMatch})

	// A cancelled subscription's channel is closed and drained.
	if _, open := <-ch; open {
		t.Error("expected the channel to be closed after cancel")
	}
	if got := h.Watchers("fam"); got != 0 {
		t.Errorf("expected no watchers, got %d", got)
	}
}

func TestCancelIsIdempotent(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe("fam")
	cancel()
	cancel() // must not panic on a double close
}

// A browser that stops reading must not be able to stall the request that is
// publishing — that request is somebody waiting on a button tap.
func TestPublishNeverBlocksOnASlowSubscriber(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe("fam")
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < bufferSize*10; i++ {
			h.Publish("fam", Event{Kind: KindMatch})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that was not reading")
	}
}

func TestConcurrentSubscribeAndPublishIsSafe(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ch, cancel := h.Subscribe("fam")
			defer cancel()
			select {
			case <-ch:
			case <-time.After(100 * time.Millisecond):
			}
		}()
		go func() {
			defer wg.Done()
			h.Publish("fam", Event{Kind: KindFamily})
		}()
	}
	wg.Wait()

	// Every subscription cancelled, so the family should be forgotten.
	if got := h.Watchers("fam"); got != 0 {
		t.Errorf("expected 0 watchers after everyone cancelled, got %d", got)
	}
}
