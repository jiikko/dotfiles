package ui

import "sync"

// pendingKeyLimit は確認・取り消しのキー (y / Enter / n / Esc) を保留する上限。超えたら古いものから捨てる。
//
// 既知の制約 (issue 586 の最終レビューで記録のみ): 送信中の先頭キーを手元に持ったまま上限超えで列の先頭を捨てるので、
// [y (送信中), Esc, …] の Esc が捨てられ、取り消しのつもりが確認になりうる。起きるのは runner が詰まっている間に
// 確認・取り消しのキーが 16 個を超えて溜まったときだけで、テストでも手元の再現でも出ていない。
// 実際に起きたら、上限超えでは「新しいキーを捨てる」(送信中と列の順序を壊さない) に変える。
// Ctrl-C は列に並べず最優先で届ける (それ以前の保留は捨てる)。既にキーのチャネルに入ったキーは追い越さないが、
// その後の Ctrl-C で必ず強制終了するので、押した順の意図からは外れない。
const pendingKeyLimit = 16

// keyDelivery keeps Bubble Tea's Update non-blocking while preserving keys
// whose loss could change or prevent a confirmed action.
type keyDelivery struct {
	out          chan<- string
	closed       <-chan struct{}
	wake         chan struct{}
	priorityWake chan struct{}
	done         chan struct{}

	mu            sync.Mutex
	queue         []string
	draining      bool
	urgentPending bool
	urgentSending bool
}

func newKeyDelivery(out chan<- string, closed <-chan struct{}) *keyDelivery {
	d := &keyDelivery{
		out: out, closed: closed,
		wake: make(chan struct{}, 1), priorityWake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go d.run()
	return d
}

func (d *keyDelivery) enqueue(key string) {
	select {
	case <-d.closed:
		return
	default:
	}
	if key == "ctrl+c" {
		d.enqueueCtrlC()
		return
	}

	d.mu.Lock()
	if d.urgentPending || d.urgentSending {
		d.mu.Unlock()
		return
	}
	if d.draining {
		if mustPreserveKey(key) {
			d.queue = appendPendingKey(d.queue, key)
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
		d.queue = appendPendingKey(d.queue, key)
		d.draining = true
		d.signal(d.wake)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
}

func (d *keyDelivery) enqueueCtrlC() {
	d.mu.Lock()
	if d.urgentPending || d.urgentSending {
		d.mu.Unlock()
		return
	}
	wasDraining := d.draining
	clear(d.queue)
	d.queue = d.queue[:0]
	d.urgentPending = true
	if !d.draining {
		d.draining = true
		d.signal(d.wake)
	}
	if wasDraining {
		d.signal(d.priorityWake)
	}
	d.mu.Unlock()
}

func (d *keyDelivery) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		var key string
		urgentKey := d.urgentPending
		if urgentKey {
			d.urgentPending = false
			d.urgentSending = true
			key = "ctrl+c"
			select {
			case <-d.priorityWake:
			default:
			}
		} else if len(d.queue) == 0 {
			d.draining = false
			d.mu.Unlock()
			select {
			case <-d.wake:
			case <-d.closed:
				return
			}
			continue
		} else {
			// Keep the head in the bounded queue until the channel accepts it.
			// That way a Ctrl-C can discard it even while the send is blocked.
			key = d.queue[0]
		}
		d.mu.Unlock()

		if urgentKey {
			select {
			case d.out <- key:
				d.mu.Lock()
				d.urgentSending = false
				if len(d.queue) == 0 {
					d.draining = false
				}
				d.mu.Unlock()
			case <-d.closed:
				return
			}
			continue
		}

		// Check once before blocking so a priority request that arrived while
		// this key was being selected is handled before the next send attempt.
		select {
		case <-d.priorityWake:
			continue
		default:
		}
		select {
		case <-d.priorityWake:
			continue
		case d.out <- key:
			d.mu.Lock()
			if len(d.queue) > 0 {
				d.queue[0] = ""
				d.queue = d.queue[1:]
			}
			if len(d.queue) == 0 && !d.urgentPending && !d.urgentSending {
				d.draining = false
			}
			d.mu.Unlock()
		case <-d.closed:
			return
		}
	}
}

func appendPendingKey(queue []string, key string) []string {
	if len(queue) < pendingKeyLimit {
		return append(queue, key)
	}
	copy(queue, queue[1:])
	queue[len(queue)-1] = key
	return queue
}

func (d *keyDelivery) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
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
