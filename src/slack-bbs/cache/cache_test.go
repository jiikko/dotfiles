package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slack-bbs/board"
	"slack-bbs/render"
)

func units(msgs ...board.Msg) []board.Unit {
	var us []board.Unit
	for _, m := range msgs {
		us = append(us, board.Unit{ID: m.Ts, Msgs: []board.Msg{m}})
	}
	return us
}

func resp(ids ...string) board.Response {
	return board.Response{Topics: []board.RespTopic{{Title: "t", State: board.StateInfo, Members: ids}}}
}

// 書いたものを読み戻せる。版の違うファイルは使わない (誤りではない)。置き場が空ならキャッシュしない。
func TestRoundTripAndVersion(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "c")}
	us := units(board.Msg{Ts: "1.000000", Text: "a", New: true})
	if err := st.SaveRaw("C1", Raw{SavedAt: time.Unix(5, 0), Page: render.Page{Channel: "C1", Base: "https://ws/"}, Units: us}); err != nil {
		t.Fatal(err)
	}
	r, ok, err := st.LoadRaw("C1")
	if err != nil || !ok || len(r.Units) != 1 || r.Page.Base != "https://ws/" {
		t.Fatalf("読み戻せない: %+v %v %v", r, ok, err)
	}
	if err := st.SaveSummary("C1", Summary{Model: "m", Basis: MsgKeys(us), Resp: resp("1.000000")}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(st.Dir, "C1.summary.json")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), fmt.Sprintf(`"version":%d`, Version), `"version":99`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.LoadSummary("C1"); ok || err != nil {
		t.Errorf("版の違う分類を使った: ok=%v err=%v", ok, err)
	}
	if _, ok, err := (Store{}).LoadRaw("C1"); ok || err != nil || (Store{}).SaveRaw("C1", Raw{}) != nil {
		t.Errorf("置き場が空なのに読み書きした")
	}
}

// 壊れたファイル・パスに使えないチャンネル ID は誤りとして返す (黙って空にしない)。
func TestLoadRejectsBrokenAndPathLikeID(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(st.Dir, "C1.raw.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.LoadRaw("C1"); ok || err == nil {
		t.Errorf("壊れたファイル: ok=%v err=%v", ok, err)
	}
	if err := st.SaveRaw("../x", Raw{}); err == nil {
		t.Errorf("パスの区切りを含む ID で書いた")
	}
}

// 前回の分類を使い回せるのは、今の発言がすべて分類したときにもあり (本文も同じ)、どの単位も話題に入っていて、モデルが同じときだけ。
func TestCovers(t *testing.T) {
	a := board.Msg{Ts: "1.000000", Text: "a"}
	b := board.Msg{Ts: "2.000000", Text: "b"}
	sum := Summary{Model: "m", Me: "U1", Basis: MsgKeys(units(a, b)), Resp: resp(a.Ts, b.Ts)}
	edited := b
	edited.Text = "b (編集)"
	for _, c := range []struct {
		name  string
		us    []board.Unit
		model string
		me    string
		want  bool
	}{
		{"同じ", units(a, b), "m", "U1", true},
		{"古い発言が外れただけ", units(b), "m", "U1", true},
		{"新しい発言", units(a, b, board.Msg{Ts: "3.000000", Text: "c"}), "m", "U1", false},
		{"編集された発言", units(a, edited), "m", "U1", false},
		{"別のモデル", units(a, b), "x", "U1", false},
		{"別の読み手", units(a, b), "m", "U2", false},
	} {
		if got := sum.Covers(c.us, c.model, c.me); got != c.want {
			t.Errorf("%s: %v (want %v)", c.name, got, c.want)
		}
	}
	// LLM が単位を落とした分類も、発言が変わらなければ使い回す (落とした単位は初回と同じく未分類に出る。毎回分類し直さない)
	partial := Summary{Model: "m", Me: "U1", Basis: sum.Basis, Resp: resp(a.Ts)}
	if !partial.Covers(units(a, b), "m", "U1") {
		t.Errorf("LLM が単位を落とした分類を使い回さない")
	}
}
