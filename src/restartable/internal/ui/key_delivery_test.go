package ui

import (
	"reflect"
	"testing"
)

func TestKeyDeliveryBoundsPendingKeysAndKeepsRecentOrder(t *testing.T) {
	const pendingKeyLimit = 16

	keys := make(chan string, 1)
	keys <- "occupied"
	closed := make(chan struct{})
	delivery := newKeyDelivery(keys, closed)
	t.Cleanup(func() {
		close(closed)
		<-delivery.done
	})

	var enqueued []string
	for i := 0; i < 48; i++ {
		key := "y"
		if i%2 == 1 {
			key = "esc"
		}
		enqueued = append(enqueued, key)
		delivery.enqueue(key)
	}

	want := enqueued[len(enqueued)-pendingKeyLimit:]
	delivery.mu.Lock()
	got := append([]string(nil), delivery.queue...)
	delivery.mu.Unlock()
	if len(got) > pendingKeyLimit {
		t.Fatalf("pending key count = %d, want at most %d", len(got), pendingKeyLimit)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending keys = %v, want most recent keys in order %v", got, want)
	}
}

func TestKeyDeliveryPrioritizesCtrlCOverPendingKeys(t *testing.T) {
	keys := make(chan string, 1)
	keys <- "occupied"
	closed := make(chan struct{})
	delivery := newKeyDelivery(keys, closed)
	t.Cleanup(func() {
		close(closed)
		<-delivery.done
	})

	for i := 0; i < 48; i++ {
		key := "y"
		if i%2 == 1 {
			key = "esc"
		}
		delivery.enqueue(key)
	}
	delivery.enqueue("ctrl+c")

	if got := <-keys; got != "occupied" {
		t.Fatalf("initial output = %q, want occupied", got)
	}
	if got := <-keys; got != "ctrl+c" {
		t.Fatalf("first delivered key after Ctrl-C = %q, want ctrl+c", got)
	}
}
