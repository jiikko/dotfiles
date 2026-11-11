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

// 既読の印は読み戻せる。印の値 (最後に読んだ ts) が ReadKeep より古い印は書くときに落とす。単位の ID (スレッドの親の ts) が
// 古くても、最近読んだ印は残す (古くに始まって今も返信の付くスレッド)。無い・版が違うファイルは空から始める。
func TestReadRoundTripAndPrune(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "c")}
	now := time.Unix(100*24*3600, 0)
	if r, err := st.LoadRead("C1"); err != nil || len(r) != 0 || r == nil {
		t.Fatalf("無いとき: %v %v", r, err)
	}
	fresh := board.TsOf(now.Add(-ReadKeep + time.Hour).Unix())
	old := board.TsOf(now.Add(-ReadKeep - time.Hour).Unix())
	if err := st.SaveRead("C1", map[string]string{old: fresh, "5.000000": old}, now); err != nil {
		t.Fatal(err)
	}
	r, err := st.LoadRead("C1")
	if err != nil || len(r) != 1 || r[old] != fresh {
		t.Fatalf("読み戻し: %v %v (古い親のスレッドの新しい印 %s を残し、古い印を落とす)", r, err, fresh)
	}
	p, _ := st.path("C1", "read")
	if err := os.WriteFile(p, []byte(`{"version":99,"units":{"1.000000":"1.000000"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := st.LoadRead("C1"); err != nil || len(r) != 0 {
		t.Errorf("版が違うとき: %v %v", r, err)
	}
	if err := (Store{}).SaveRead("C1", map[string]string{fresh: "1"}, now); err != nil {
		t.Errorf("置き場が空なら書かない: %v", err)
	}
}

// Expire は書き換えの無いまま期間を過ぎたファイルだけを消す: raw と summary は組の新しい方から CacheKeep、既読の印は ReadKeep、
// 書きかけの一時ファイルは CacheKeep。
// キャッシュの形でないファイル (slack-cli の lock・知らない拡張子) と、期間の内のファイルには触らない。
func TestExpire(t *testing.T) {
	dir := t.TempDir()
	st := Store{Dir: dir}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	age := map[string]time.Duration{
		"COLD.raw.json":         CacheKeep + time.Hour, // 消す
		"COLD.summary.json":     CacheKeep + time.Hour, // 消す
		"USED.summary.json":     CacheKeep * 3,         // 分類を使い回していて raw だけが新しい: 組で数えて残す
		"USED.raw.json":         time.Hour,
		"COLD.raw.json.tmp.123": CacheKeep + time.Hour, // 書きかけで残った一時ファイル: 消す
		"WARM.raw.json.tmp.456": time.Hour,
		"USED.raw.json.tmp.789": CacheKeep + time.Hour, // 組 (USED) が新しくても、古い一時ファイルは消す
		"channels.json.tmp.321": CacheKeep + time.Hour, // チャンネルの一覧の書きかけも消す
		"view.json.tmp.987":     CacheKeep + time.Hour, // 最後に見ていたタブの書きかけも消す
		"view.json":             ReadKeep * 2,          // 最後に見ていたタブは消さない
		"channels.json":         ReadKeep * 2,          // 一覧そのものは購読の画面を開くたびに書き直すので残す
		"notes.json.tmp.654":    ReadKeep * 2,          // キャッシュの形でない一時ファイルには触らない
		"COLD.read.json":        CacheKeep + time.Hour, // 既読は ReadKeep まで残す
		"OLD.read.json":         ReadKeep + time.Hour,  // 消す
		"OLD.hidden.json":       ReadKeep + time.Hour,  // 消す (隠した話題も既読と同じ期限)
		"COLD.hidden.json":      CacheKeep + time.Hour,
		"WARM.raw.json":         CacheKeep - time.Hour,
		"slack-cli.lock":        ReadKeep * 2,
		"notes.json":            ReadKeep * 2,
		"x.y.json":              ReadKeep * 2, // ID の形でない
	}
	for name, a := range age {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-a), now.Add(-a)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := st.Expire(now)
	if err != nil || n != 8 {
		t.Fatalf("Expire = %d, %v; want 8 件", n, err)
	}
	var left []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		left = append(left, e.Name())
	}
	want := "COLD.hidden.json,COLD.read.json,USED.raw.json,USED.summary.json,WARM.raw.json,WARM.raw.json.tmp.456,channels.json,notes.json,notes.json.tmp.654,slack-cli.lock,view.json,x.y.json"
	if got := strings.Join(left, ","); got != want {
		t.Errorf("残ったファイル: %s, want %s", got, want)
	}
	if n, err := (Store{Dir: filepath.Join(dir, "none")}).Expire(now); n != 0 || err != nil {
		t.Errorf("置き場が無いとき: %d, %v", n, err)
	}
}

// 消す直前に取り直した時刻で、組のどれか 1 つでも期限の内に書き直されていたら、組ごと残す (raw だけ新しくなった組を割らない)。
func TestExpireKeepsPairTouchedJustBefore(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	at := map[string]time.Time{"C1.raw.json": now.Add(-time.Minute), "C1.summary.json": now.Add(-CacheKeep * 2)}
	g := &expiring{names: []string{"C1.summary.json", "C1.raw.json"}, keep: CacheKeep}
	mtime := func(name string) (time.Time, bool) { t, ok := at[name]; return t, ok }
	if !g.touched(now, mtime) {
		t.Error("raw が書き直された組を、消してよい組と判定した")
	}
	at["C1.raw.json"] = now.Add(-CacheKeep * 2)
	if g.touched(now, mtime) {
		t.Error("どれも古い組を、書き直された組と判定した")
	}
}
