package disk

import (
	"strings"
	"testing"
	"time"

	"termsafe/ctlprobe"
)

// 検出条件そのものが未実測のエントリ (Entry.Unverified) は、候補 0 件でも行を畳まない。
// 畳むと「名前が違って 1 件も当たらなかった」が「候補なし = きれい」と同じ見え方になり、
// 探せていないことが画面から消える (issue 169 / 207)。
func TestFormatKeepsUnverifiedEntryWithZeroItems(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	unver := Result{
		Entry: Entry{ID: "u", Label: "未実測の項目", Risk: RiskSafe, DeleteVia: "rm",
			Recover: "再生成されません", Unverified: "ファイル名が未実測"},
		Status: StatusOK, Items: []Item{},
	}
	// 対照: 同じ 0 件でも Unverified が無ければ畳まれる
	verified := Result{
		Entry:  Entry{ID: "v", Label: "実測済みの項目", Risk: RiskSafe, DeleteVia: "rm", Recover: "再生成されません"},
		Status: StatusOK, Items: []Item{},
	}
	out := Format(Report{Results: []Result{unver, verified}}, Env{}, now)

	if !strings.Contains(out, "未実測の項目") {
		t.Errorf("未実測のエントリが 0 件で畳まれている (探せていないことが画面から消える):\n%s", out)
	}
	if !strings.Contains(out, "🔎 未検証") {
		t.Errorf("未実測のエントリに専用のマークが出ていない (✅ 安全 だと『調べたうえで安全』と読める):\n%s", out)
	}
	if strings.Contains(out, "✅ 安全") {
		t.Errorf("未実測のエントリに『✅ 安全』が出ている (調べられていないので嘘になる):\n%s", out)
	}
	if !strings.Contains(out, `0 件ですが「候補なし」ではありません`) {
		t.Errorf("0 件の意味を説明する行が無い:\n%s", out)
	}
	// Recover は出さない (消す対象が 0 件なのに復元方法を出すと検出できているように読める)
	if strings.Contains(out, "再生成されません") {
		t.Errorf("0 件の未実測エントリに復元方法が出ている (検出できているように読める):\n%s", out)
	}
	if strings.Contains(out, "実測済みの項目") {
		t.Errorf("Unverified の無い 0 件エントリまで表示されている (畳む側の規律が壊れている):\n%s", out)
	}
	if strings.Contains(out, "掃除の候補はありませんでした") {
		t.Errorf("未検証の行が在るのに『候補はありませんでした』と締めている:\n%s", out)
	}
}

// Mark が返す語彙を固定する。**この 6 語が CLI と TUI の唯一の出典**なので、
// ここを変えると両方の画面の文言が同時に変わる (issue 222 で 2 実装を 1 つに寄せた)。
// 🚨 変異検証 2026-09-04: 寄せた直後は「注意 / 要確認 / 対象外」を pin するテストがどちらの
// module にも無く、語を書き換えても両方緑のままだった (このテストはその穴を塞ぐ)。
func TestMarkVocabulary(t *testing.T) {
	safe := Entry{ID: "e", Risk: RiskSafe}
	for _, tc := range []struct {
		name string
		r    Result
		want string
	}{
		{"blocked", Result{Entry: safe, Status: StatusBlocked}, "🚫 対象外"},
		{"failed", Result{Entry: safe, Status: StatusFailed}, "❓ 走査できず"},
		{"unverified", Result{Entry: Entry{ID: "e", Risk: RiskSafe, Unverified: "名前が未確定"}, Status: StatusOK}, "🔎 未検証"},
		{"safe", Result{Entry: safe, Status: StatusOK, Items: []Item{{Path: "/p"}}}, "✅ 安全"},
		{"caution", Result{Entry: Entry{ID: "e", Risk: RiskCaution}, Status: StatusOK, Items: []Item{{Path: "/p"}}}, "🚨 注意"},
		{"confirm", Result{Entry: Entry{ID: "e", Risk: RiskConfirm}, Status: StatusOK, Items: []Item{{Path: "/p"}}}, "⛔ 要確認"},
	} {
		if got := Mark(tc.r); got != tc.want {
			t.Errorf("%s: Mark = %q; want %q", tc.name, got, tc.want)
		}
	}
}

// Foldable は「候補 0 件で畳んでよい行か」の唯一の出典。畳む条件が緩むと候補のある行が消え、
// 締まると未検証の行が「候補なし」と同じ見え方になる (false green。issue 169 / 207)。
func TestFoldable(t *testing.T) {
	base := Result{Entry: Entry{ID: "e"}, Status: StatusOK}
	if !Foldable(base) {
		t.Error("候補 0 件・失敗 0 件・未検証でない行は畳む")
	}
	withItem := base
	withItem.Items = []Item{{Path: "/p"}}
	if Foldable(withItem) {
		t.Error("候補がある行を畳んだ")
	}
	withFail := base
	withFail.Failures = []string{"走査できず"}
	if Foldable(withFail) {
		t.Error("一部走査できなかった行を畳んだ")
	}
	unver := base
	unver.Entry.Unverified = "名前が未確定"
	if Foldable(unver) {
		t.Error("検出条件が未実測の行を畳んだ (false green)")
	}
	if Foldable(Result{Entry: Entry{ID: "e"}, Status: StatusBlocked}) {
		t.Error("blocked の行を畳んだ (理由を出す必要がある)")
	}
}

// CLI (diskdoctor) は stdout へ**直接**書くので、ここが最後の関門 (issue 228)。
// TUI の描画層はセル単位に分解するため端末制御そのものは落ちるが、この経路にはその後段が無い:
// OSC52 (クリップボード書き込み) やタイトル書き換えが「表示しただけ」で発火する。
//
// 🚨 材料は実在のファイル名と ReadDir の名前。macOS のファイル名は `/` と NUL 以外を許し、
// カタログの対象 ($TMPDIR / ~/Library/Caches) は誰でも書ける場所なので、細工した名前の
// ディレクトリを 1 つ置けば注入できる。
func TestFormatSanitizesUntrustedText(t *testing.T) {
	const esc, bel = "\x1b", "\a"
	osc52 := esc + "]52;c;cHduZWQ=" + bel
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	// 2 エントリに分ける: Format はパス一覧と中身一覧 (Inspect) を**別の分岐**で出すので、
	// 片方だけの fixture では他方の経路を 1 度も踏まない
	rep := Report{Results: []Result{{
		Entry: Entry{ID: "x", Label: "細工" + osc52, Risk: RiskSafe, DeleteVia: "rm",
			Recover: "再生成" + esc + "[2J されます"},
		Status: StatusOK, Size: 12288,
		Items: []Item{{Path: "/tmp/ok", Size: 4096}, {Path: "/tmp/ev" + osc52 + "il", Size: 8192},
			{Path: "/tmp/ok2", Size: 4096}},
		Failures: []string{"読めず" + esc + "[31m"},
	}, {
		Entry: Entry{ID: "y", Label: "中身を見せる項目", Risk: RiskConfirm, DeleteVia: "trash",
			Recover: "戻せません", Inspect: true},
		Status: StatusOK, Size: 4096,
		Items:    []Item{{Path: "/tmp/inspect", Size: 4096}},
		Contents: []string{"inspect/child" + esc + "[2Jname"},
	}}}
	rep.Total = SumDeletable(rep.Results)

	out := Format(rep, Env{}, now)
	for _, line := range strings.Split(out, "\n") {
		// 判定の正本は termsafe/ctlprobe (issue 285)。ここへ書き戻さないこと —
		// 同じオラクルが 5 箇所に複製されていて、無害化の定義を広げるときに
		// 直し忘れた側だけが旧い狭い判定で守り続ける形になっていた。
		if ctlprobe.HasControl(line) {
			t.Fatalf("stdout へ制御シーケンスが出た: %q", line)
		}
	}
	if strings.Contains(out, "cHduZWQ=") {
		t.Errorf("OSC の中身が本文として残った: %q", out)
	}
	// 制御文字入りのパスは落とす。落としたことは黙らせない (行数と合計が理由なく減る)
	if strings.Contains(out, "/tmp/ev") {
		t.Errorf("制御文字入りのパスが出た: %q", out)
	}
	if !strings.Contains(out, "制御文字") {
		t.Errorf("落としたことが出力に残っていない: %q", out)
	}
	if !strings.Contains(out, "/tmp/ok") {
		t.Errorf("綺麗なパスまで落ちた: %q", out)
	}
}
