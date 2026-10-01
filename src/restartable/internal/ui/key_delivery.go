package ui

import "sync"

// keyDelivery keeps Bubble Tea's Update non-blocking while preserving keys
// whose loss could change or prevent a confirmed action.
type keyDelivery struct {
	out    chan<- string
	closed <-chan struct{}
	wake   chan struct{}
	done   chan struct{}

	mu       sync.Mutex
	queue    []string
	draining bool
}

func newKeyDelivery(out chan<- string, closed <-chan struct{}) *keyDelivery {
	d := &keyDelivery{out: out, closed: closed, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go d.run()
	return d
}

func (d *keyDelivery) enqueue(key string) {
	select {
	case <-d.closed:
		return
	default:
	}
	d.mu.Lock()
	if d.draining {
		if mustPreserveKey(key) {
			d.queue = append(d.queue, key)
		}
		d.mu.Unlock()
		return
	}
	select {
	case d.out <- key:
		d.mu.Unlock()
		return
	default:
	}
	if mustPreserveKey(key) {
		d.queue = append(d.queue, key)
		d.draining = true
		d.mu.Unlock()
		select {
		case d.wake <- struct{}{}:
		default:
		}
		return
	}
	d.mu.Unlock()
}

func (d *keyDelivery) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.draining = false
			d.mu.Unlock()
			select {
			case <-d.wake:
			case <-d.closed:
				return
			}
			continue
		}
		key := d.queue[0]
		d.queue[0] = ""
		d.queue = d.queue[1:]
		d.mu.Unlock()

		select {
		case d.out <- key:
		case <-d.closed:
			return
		}

		d.mu.Lock()
		if len(d.queue) == 0 {
			d.draining = false
		}
		d.mu.Unlock()
	}
}

func mustPreserveKey(key string) bool {
	switch key {
	case "ctrl+c", "esc", "y", "Y", "enter", "n", "N":
		return true
	default:
		return false
	}
}
