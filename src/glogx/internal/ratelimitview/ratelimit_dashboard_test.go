package ratelimitview

import (
	"errors"
	"glogx/internal/app"
	"glogx/internal/term"
	"ratelimit/usage"
	"strings"
	"testing"
	"time"
)

func rlTestOpts(snap *usage.Snapshot, err error) RatelimitRenderOpts {
	return RatelimitRenderOpts{
		Width: 100, Page: 30, Colored: false, Spinner: "⠋",
		Snap: snap, Err: err,
		Now: time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local),
	}
}

// 全画面 viewer の契約: lines はどの状態でもちょうど page 行を返し、どの行も width を
// 超えない (超えると finishWindow が枠を組めず画面が折り返す)。
func TestRatelimitDashLinesFillsPage(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	states := map[string]RatelimitRenderOpts{
		"取得済み": rlTestOpts(rlTestSnap(), nil),
		"取得中":  rlTestOpts(nil, nil),
		"失敗":   rlTestOpts(nil, errors.New("boom")),
	}
	for name, o := range states {
		for _, page := range []int{30, 12, 4, 1} {
			o.Page = page
			got := d.Lines(o)
			if len(got) != page {
				t.Errorf("%s page=%d: 行数 %d", name, page, len(got))
			}
			for i, ln := range got {
				if w := term.DispWidth(ln); w > o.Width {
					t.Errorf("%s page=%d: %d 行目の幅 %d > %d", name, page, i, w, o.Width)
				}
			}
		}
	}
}

// 取得中はスピナー、失敗は理由を出す (盤が出ないまま無言にならない)。
func TestRatelimitDashLoadingAndError(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	if got := strings.Join(d.Lines(rlTestOpts(nil, nil)), "\n"); !strings.Contains(got, "取得中") {
		t.Errorf("取得中の表示が無い:\n%s", got)
	}
	if got := strings.Join(d.Lines(rlTestOpts(nil, errors.New("x"))), "\n"); !strings.Contains(got, "取得失敗") {
		t.Errorf("失敗の表示が無い:\n%s", got)
	}
}

// last-good があるときは、取得に失敗していても盤を出し続ける (右上オーバーレイと同じ契約)。
func TestRatelimitDashKeepsLastGoodOnError(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	got := strings.Join(d.Lines(rlTestOpts(rlTestSnap(), errors.New("x"))), "\n")
	if strings.Contains(got, "取得失敗") {
		t.Errorf("last-good があるのに失敗表示へ落ちた:\n%s", got)
	}
	if !strings.Contains(got, "復活まで") {
		t.Errorf("last-good の盤が出ていない:\n%s", got)
	}
}

// 見出しには両 CLI のバージョンが出る (右上オーバーレイと同じ情報量)。
func TestRatelimitDashHeaderShowsVersions(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	snap := rlTestSnap()
	snap.CodexVersion = "0.144.6"
	snap.Windows = append(snap.Windows, usage.Window{
		Label: "cx5h", Source: usage.SourceCodex, Percent: 10,
		ResetAt: time.Now().Add(time.Hour), WindowMins: 300,
	})
	head := d.Lines(rlTestOpts(snap, nil))[0]
	for _, want := range []string{"v2.1.216", "codex v0.144.6", "ratelimit"} {
		if !strings.Contains(head, want) {
			t.Errorf("見出しに %q が無い: %q", want, head)
		}
	}
}

// Claude 側だけ取れなかったとき (codex だけ取れ、Claude 枠は前回値のまま) は、
// 古い残量を黙って出さず理由を見出しの下に出す。理由は外部コマンドの stderr なので無害化する。
func TestRatelimitDashShowsClaudeFetchError(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	if got := d.Lines(rlTestOpts(rlTestSnap(), nil))[1]; got != "" {
		t.Fatalf("取得できているのに注記行がある: %q", got)
	}
	snap := rlTestSnap()
	snap.ClaudeErr = "claude /usage 実行失敗: exit status 127: nodenv: \x1b[31mcommand not found"
	o := rlTestOpts(snap, nil)
	lines := d.Lines(o)
	note := term.StripANSI(lines[1])
	for _, want := range []string{"Claude Code の取得に失敗", "前回の値を表示中", "exit status 127"} {
		if !strings.Contains(note, want) {
			t.Errorf("注記に %q が無い: %q", want, note)
		}
	}
	if strings.Contains(lines[1], "\x1b[31m") {
		t.Errorf("理由の制御列が無害化されていない: %q", lines[1])
	}
	if len(lines) != o.Page {
		t.Errorf("行数 = %d, want %d", len(lines), o.Page)
	}
	for i, l := range lines {
		if w := term.DispWidth(l); w > o.Width {
			t.Errorf("%d 行目が幅 %d を超える (%d): %q", i, o.Width, w, l)
		}
	}
}

// 閉じる / 更新 / 横断以外のキーは飲み切る (全画面なので裏の一覧をスクロールさせない)。
func TestRatelimitDashHandleKey(t *testing.T) {
	for _, key := range []string{"R", "q", "esc", "h", "left"} {
		var d RatelimitDash
		d.Toggle()
		if got := d.HandleKey(key); got != RlDashClosed {
			t.Errorf("%q: action=%v, want rlDashClosed", key, got)
		}
		if d.Visible() {
			t.Errorf("%q で閉じていない", key)
		}
	}
	var d RatelimitDash
	d.Toggle()
	if got := d.HandleKey("r"); got != RlDashRefresh {
		t.Errorf("r: action=%v, want rlDashRefresh", got)
	}
	if !d.Visible() {
		t.Error("r で閉じてしまった")
	}
	// i / s / D は他の全画面への横断 (ユーザー要望 2026-09-01)。🚨 横断でも自分は閉じる:
	// 開いたまま viewer を開くと「見えている画面」と「キーを受ける画面」が食い違う。
	for _, tc := range []struct {
		key  string
		want app.FullScreenID
	}{{"i", app.FullScreenIssues}, {"s", app.FullScreenStatus}, {"D", app.FullScreenDoctor}} {
		var d RatelimitDash
		d.Toggle()
		if got := d.HandleKey(tc.key); got != RlDashCross {
			t.Errorf("%q: action=%v, want rlDashCross", tc.key, got)
		}
		if got := d.TakeWantCross(); got != tc.want {
			t.Errorf("%q: 横断先=%v, want %v", tc.key, got, tc.want)
		}
		if d.Visible() {
			t.Errorf("%q の横断でダッシュボードが閉じていない", tc.key)
		}
	}
	d = RatelimitDash{}
	d.Toggle()
	for _, key := range []string{"j", "k", "b", "u", "enter"} {
		if got := d.HandleKey(key); got != RlDashSwallow {
			t.Errorf("%q を飲み込んでいない: action=%v", key, got)
		}
	}
	if !d.Visible() {
		t.Error("飲み込むキーで閉じてしまった")
	}
}

// 取得できていても「描ける枠が 1 つも無い」ことがある (Claude 側は既定の枠ラベルでしか
// 拾わないので、/usage の文言が変わると起こる)。全画面なので、無言の白画面にせず理由を出す。
func TestRatelimitDashNoRenderableWindows(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	snap := &usage.Snapshot{Windows: []usage.Window{
		{Label: "Current opus quota", Percent: 5, ResetAt: time.Now().Add(time.Hour)},
	}}
	got := strings.Join(d.Lines(rlTestOpts(snap, nil)), "\n")
	if !strings.Contains(got, "表示できる利用枠がありません") {
		t.Errorf("無言の白画面になっている:\n%s", got)
	}
}

// 見出しは狭い端末でも width を超えない。フレームが自動 OFF になる帯ではクリップが
// 効かないので、超えると折り返して画面全体が崩れる。
func TestRatelimitDashHeaderFitsWidth(t *testing.T) {
	var d RatelimitDash
	d.Toggle()
	snap := rlTestSnap()
	snap.CodexVersion = "0.144.6"
	snap.Windows = append(snap.Windows, usage.Window{
		Label: "cx5h", Source: usage.SourceCodex, Percent: 10,
		ResetAt: time.Now().Add(time.Hour), WindowMins: 300,
	})
	for w := 1; w <= 120; w++ {
		o := rlTestOpts(snap, nil)
		o.Width, o.Page = w, 30
		for i, ln := range d.Lines(o) {
			if got := term.DispWidth(ln); got > w {
				t.Errorf("w=%d: %d 行目の幅 %d: %q", w, i, got, ln)
			}
		}
	}
}

// 盤のメモ が「出力を変えない」ことを、メモ無しの結果との差分で固定する。
//
// 🚨 キャッシュの正しさは「速いか」ではなく「**同じ入力で同じ出力か**」でしか確かめられない。
// 各ケースで dropCache() した結果 (= 素の描画) と、キャッシュを温めた結果を突き合わせる。
func TestRatelimitDashCacheMatchesUncached(t *testing.T) {
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	snapA, snapB := rlTestSnap(), rlTestSnap()
	cases := []struct {
		name string
		o    RatelimitRenderOpts
	}{
		{"基準", RatelimitRenderOpts{Width: 120, Page: 40, Colored: false, Snap: snapA, Now: base}},
		{"幅ちがい", RatelimitRenderOpts{Width: 100, Page: 40, Colored: false, Snap: snapA, Now: base}},
		{"高さちがい", RatelimitRenderOpts{Width: 120, Page: 30, Colored: false, Snap: snapA, Now: base}},
		{"色つき", RatelimitRenderOpts{Width: 120, Page: 40, Colored: true, Snap: snapA, Now: base}},
		{"秒がちがう", RatelimitRenderOpts{Width: 120, Page: 40, Colored: false, Snap: snapA, Now: base.Add(time.Second)}},
		{"snap がちがう", RatelimitRenderOpts{Width: 120, Page: 40, Colored: false, Snap: snapB, Now: base}},
	}
	var d RatelimitDash
	d.shown = true
	for _, c := range cases {
		d.dropCache()
		want := d.Lines(c.o) // メモ無し (直前に捨てた)
		d.dropCache()
		_ = d.Lines(c.o)    // 温める
		got := d.Lines(c.o) // ここがキャッシュ経路
		if !d.cacheOK {
			t.Fatalf("%s: キャッシュが立っていない (この経路を測れていない)", c.name)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: 行数が違う %d != %d", c.name, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: %d 行目が違う\n  memo=%q\n  素  =%q", c.name, i, got[i], want[i])
			}
		}
	}

	// 鍵に入っている入力を 1 つ変えたら、必ずキャッシュを外して描き直す。
	// 🚨 比較は**常に基準ケースとの差**で見る。直前のケースと比べる形にすると、
	// 各ケースが 2 箇所ずつ違うせいで「鍵から 1 項目落とす」変異を素通しする
	// (最初にそう書いて踏んだ: sec を鍵から外しても緑だった)。
	for _, c := range cases[1:] {
		d.dropCache()
		_ = d.Lines(cases[0].o) // 基準を焼く
		baseKey := d.cacheKey
		_ = d.Lines(c.o)
		if d.cacheKey == baseKey {
			t.Errorf("%s: 基準と入力が違うのにキャッシュ鍵が同じ (描き直していない)", c.name)
		}
	}

	// 返すのはコピー (呼び出し側が書き換えてもキャッシュが壊れない)。
	// 🚨 書き換えるのは**キャッシュヒットの戻り値**にすること。ミス経路の戻り値は
	// キャッシュの裏地ではないので、そちらを書き換えても何も検出できない (これも踏んだ)。
	d.dropCache()
	_ = d.Lines(cases[0].o) // ミス (ここで焼く)
	hit := d.Lines(cases[0].o)
	if !d.cacheOK {
		t.Fatal("キャッシュが立っていない")
	}
	hit[0] = "PWNED"
	if again := d.Lines(cases[0].o); again[0] == "PWNED" {
		t.Error("キャッシュの裏地を返している (呼び出し側の書き換えが次のフレームへ漏れる)")
	}
}
