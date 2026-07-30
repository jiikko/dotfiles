package issues

import "testing"

func TestBodyCachesPerWidth(t *testing.T) {
	b := NewBody(sample)
	first := b.Lines(60, false)
	if b.renders != 1 {
		t.Fatalf("初回で整形されていない: renders=%d", b.renders)
	}
	if got := b.Lines(60, false); b.renders != 1 || len(got) != len(first) {
		t.Fatalf("同じ幅でキャッシュが効いていない: renders=%d", b.renders)
	}
	if b.Lines(40, false); b.renders != 2 {
		t.Fatalf("幅が変わったのに再整形されていない: renders=%d", b.renders)
	}
	if b.Lines(40, true); b.renders != 3 {
		t.Fatalf("colored が変わったのに再整形されていない: renders=%d", b.renders)
	}
	if b.Len() == 0 {
		t.Fatal("Len が整形後も 0")
	}
}
