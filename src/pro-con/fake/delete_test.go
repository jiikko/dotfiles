package fake

import (
	"errors"
	"strings"
	"testing"

	"pro-con/backend"
)

func has(s *Sim, id string) bool {
	for _, c := range s.Snapshot().Cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

// 模擬も本物のモードと同じ振る舞い: 依頼の列のカードはすぐ消え、作業中のカードは削除中になって次の刻みで PG を止めてから消える。
func TestDeleteMatchesLiveMode(t *testing.T) {
	s := New(t0)
	if _, err := s.Apply(backend.DeleteCard{CardID: "C-001"}); err != nil || has(s, "C-001") {
		t.Fatalf("依頼の列のカードがすぐ消えない: %v", err)
	}
	if _, err := s.Apply(backend.DeleteCard{CardID: "C-005", From: "人間"}); err != nil {
		t.Fatal(err)
	}
	c := get(t, s, "C-005")
	if !c.Deleting() || c.DeleteBy != "人間" {
		t.Fatalf("作業中のカードに削除の印が付かない: %+v", c)
	}
	if _, err := s.Apply(backend.AddOrder{CardID: "C-005", Text: "別件", Kind: 2}); !errors.Is(err, backend.ErrDeleting) {
		t.Fatalf("削除中のカードに別件の子を作った: %v", err)
	}
	s.Step()
	if has(s, "C-005") {
		t.Fatal("次の刻みで消えない")
	}
	for _, cons := range s.Snapshot().Consumers {
		if cons.CardID == "C-005" {
			t.Fatal("消したカードの PG が動いたまま")
		}
	}
	if vs := s.Snapshot().Violations; len(vs) != 0 {
		t.Fatalf("削除で不変条件を破った: %v", vs)
	}
}

// 子カードの親は消さない (子が親を失う)。
func TestDeleteRefusesParent(t *testing.T) {
	s := New(t0)
	if _, err := s.Apply(backend.DeleteCard{CardID: "C-010"}); err == nil || !strings.Contains(err.Error(), "C-004") || get(t, s, "C-010").Deleting() {
		t.Fatalf("子カードのある親の削除を受けた: %v", err)
	}
}
