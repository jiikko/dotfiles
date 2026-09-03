package disk

import (
	"strings"
	"testing"
	"time"
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
	out := Format(Report{Results: []Result{unver, verified}}, now)

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