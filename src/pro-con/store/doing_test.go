package store

import (
	"testing"
	"time"

	"pro-con/card"
)

// 完了したカードには前の様子を足さない (完了してから dispatcher が次に集めるまでの間も、完了と「走っている」を並べない)。
func TestAttachSkipsDoneCard(t *testing.T) {
	d := Doing{At: time.Now(), Cards: map[string][]card.Doing{"C-001": {{Kind: card.DoingProcess, Text: "make test"}}}}
	running := card.Card{ID: "C-001", State: card.Running}
	d.Attach(&running)
	if len(running.Doing) != 1 {
		t.Fatalf("作業中のカードに足さない: %+v", running)
	}
	done := card.Card{ID: "C-001", State: card.Done}
	d.Attach(&done)
	if len(done.Doing) != 0 || !done.DoingAt.IsZero() {
		t.Fatalf("完了したカードに足した: %+v", done)
	}
}
