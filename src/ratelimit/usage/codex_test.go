package usage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexRateLimitsJSON は実機の app-server 応答 (2026-07-31 取得) の result 部。
const codexRateLimitsJSON = `{"rateLimits":{"limitId":"codex","limitName":null,"primary":{"usedPercent":69,"windowDurationMins":10080,"resetsAt":1785903020},"secondary":null,"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"planType":"plus","rateLimitReachedType":null}}`

func TestParseCodexRateLimits(t *testing.T) {
	ws, err := parseCodexRateLimits([]byte(codexRateLimitsJSON))
	if err != nil {
		t.Fatalf("parseCodexRateLimits: %v", err)
	}
	if len(ws) != 1 {
		t.Fatalf("枠数 = %d, want 1 (secondary は null)", len(ws))
	}
	w := ws[0]
	if w.Label != "cx7d" {
		t.Errorf("Label = %q, want cx7d", w.Label)
	}
	if w.Source != SourceCodex {
		t.Errorf("Source = %q, want %q", w.Source, SourceCodex)
	}
	if w.Percent != 69 {
		t.Errorf("Percent = %d, want 69", w.Percent)
	}
	if !w.ResetAt.Equal(time.Unix(1785903020, 0)) {
		t.Errorf("ResetAt = %v, want %v", w.ResetAt, time.Unix(1785903020, 0))
	}
}

func TestParseCodexRateLimitsSecondaryAndFloat(t *testing.T) {
	// usedPercent が float で来ても丸めて取り込む (rollout ログでは 8.0 形式を観測)。
	// secondary があれば 2 枠になる。
	in := `{"rateLimits":{"primary":{"usedPercent":8.6,"windowDurationMins":300,"resetsAt":100},"secondary":{"usedPercent":42,"windowDurationMins":10080,"resetsAt":200}}}`
	ws, err := parseCodexRateLimits([]byte(in))
	if err != nil {
		t.Fatalf("parseCodexRateLimits: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("枠数 = %d, want 2", len(ws))
	}
	if ws[0].Label != "cx5h" || ws[0].Percent != 9 {
		t.Errorf("primary = %q/%d, want cx5h/9", ws[0].Label, ws[0].Percent)
	}
	if ws[1].Label != "cx7d" || ws[1].Percent != 42 {
		t.Errorf("secondary = %q/%d, want cx7d/42", ws[1].Label, ws[1].Percent)
	}
}

func TestParseCodexRateLimitsNullResetsAtIsUnused(t *testing.T) {
	// resetsAt null は「まだ消費が始まっていない」枠 (窓が開いていないので締め切りが無い)。
	// 捨てると 5h を使っていない時間帯にカードが黙って消える (ユーザー報告 2026-09-03)。
	in := `{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":null},"secondary":{"usedPercent":42,"windowDurationMins":10080,"resetsAt":200}}}`
	ws, err := parseCodexRateLimits([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 {
		t.Fatalf("len = %d, want 2 (未消費の枠を捨てている)", len(ws))
	}
	if !ws[0].Unused || !ws[0].ResetAt.IsZero() || ws[0].Label != "cx5h" || ws[0].WindowMins != 300 {
		t.Errorf("primary = %+v, want Unused/cx5h/300min/ResetAt ゼロ", ws[0])
	}
	if ws[1].Unused {
		t.Errorf("secondary が Unused になっている: %+v", ws[1])
	}
}

func TestParseCodexRateLimitsEmpty(t *testing.T) {
	if _, err := parseCodexRateLimits([]byte(`{"rateLimits":{}}`)); err == nil {
		t.Error("枠なしでエラーにならなかった")
	}
	if _, err := parseCodexRateLimits([]byte(`not json`)); err == nil {
		t.Error("非 JSON でエラーにならなかった")
	}
}

func TestCodexLabel(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	cases := []struct {
		mins *int64
		want string
	}{
		{i(300), "cx5h"},
		{i(10080), "cx7d"},
		{i(1440), "cx1d"},
		{i(90), "cx90m"},
		{nil, "cx"},
	}
	for _, c := range cases {
		if got := codexLabel(c.mins); got != c.want {
			t.Errorf("codexLabel(%v) = %q, want %q", c.mins, got, c.want)
		}
	}
}

func TestParseCodexRPCLine(t *testing.T) {
	// 応答行以外 (通知・server 発の要求・別 id・非 JSON) は found=false で読み飛ばす。
	skips := []string{
		`{"method":"remoteControl/status/changed","params":{"status":"disabled"}}`,
		`{"id":2,"method":"loginChatGptComplete","params":{}}`, // id が同値でも method 付き = server 発要求
		`{"id":1,"result":{"userAgent":"x"}}`,
		`not json at all`,
	}
	for _, line := range skips {
		if _, found, _ := parseCodexRPCLine([]byte(line)); found {
			t.Errorf("読み飛ばすべき行を応答と誤認: %s", line)
		}
	}
	// エラー応答は found=true + err。
	_, found, err := parseCodexRPCLine([]byte(`{"id":2,"error":{"code":-32600,"message":"boom"}}`))
	if !found || err == nil {
		t.Errorf("エラー応答: found=%v err=%v, want true/non-nil", found, err)
	}
	// 正常応答。
	ws, found, err := parseCodexRPCLine([]byte(`{"id":2,"result":` + codexRateLimitsJSON + `}`))
	if !found || err != nil || len(ws) != 1 {
		t.Errorf("正常応答: found=%v err=%v 枠数=%d, want true/nil/1", found, err, len(ws))
	}
}

// writeStub は PATH 差し替え用の fake CLI スクリプトを dir へ置く。
func writeStub(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// codexStubOK は実サーバの応答順 (initialize 応答 → 通知 → rateLimits 応答) を模す。
// FetchCodex が通知を読み飛ばし id=2 の応答だけを拾うこと、stdin を開いたまま応答を
// 待つ配管が成立していることの結合テスト用。
const codexStubOK = `read _l1
printf '%s\n' '{"id":1,"result":{"userAgent":"fake"}}'
printf '%s\n' '{"method":"remoteControl/status/changed","params":{"status":"disabled"}}'
read _l2
read _l3
printf '%s\n' '{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":69,"windowDurationMins":10080,"resetsAt":1785903020},"secondary":null}}}'
`

func TestFetchCodex(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "codex", codexStubOK)
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := FetchCodex(ctx)
	if err != nil {
		t.Fatalf("FetchCodex: %v", err)
	}
	if len(ws) != 1 || ws[0].Label != "cx7d" || ws[0].Percent != 69 {
		t.Errorf("ws = %+v, want cx7d/69 の 1 枠", ws)
	}
}

func TestFetchCodexNoResponse(t *testing.T) {
	// 応答を返さず終了する server (プロトコル変更・未ログイン等の縮退) はエラーに落ちる。
	dir := t.TempDir()
	writeStub(t, dir, "codex", "read _l1\nexit 0\n")
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := FetchCodex(ctx); err == nil {
		t.Error("無応答終了でエラーにならなかった")
	}
}

// claudeStubOK は claude CLI の /usage と --version を模す (Fetch の期待する JSON 形)。
const claudeStubOK = `case "$1" in
--version) printf '%s\n' "9.9.9 (Claude Code)";;
*) printf '%s' '{"result":"Current session: 2% used ` + "·" + ` resets Jul 22 at 3:09am (Asia/Tokyo)\nCurrent week (all models): 29% used ` + "·" + ` resets Jul 24 at 8am (Asia/Tokyo)","is_error":false}';;
esac
`

func TestFetchAllMergesBothSources(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "claude", claudeStubOK)
	writeStub(t, dir, "codex", codexStubOK)
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := FetchAll(ctx)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	for _, label := range []string{"5h", "7d", "cx7d"} {
		if _, ok := snap.Find(label); !ok {
			t.Errorf("%s 枠がない: %+v", label, snap.Windows)
		}
	}
	if snap.Version != "9.9.9" {
		t.Errorf("Version = %q, want 9.9.9", snap.Version)
	}
	if !snap.HasCodex() {
		t.Error("HasCodex() = false, want true")
	}
}

func TestFetchAllCodexFailureKeepsClaude(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "claude", claudeStubOK)
	writeStub(t, dir, "codex", "exit 1\n")
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := FetchAll(ctx)
	if err != nil {
		t.Fatalf("codex 失敗が FetchAll 全体を失敗させた: %v", err)
	}
	if _, ok := snap.Find("5h"); !ok {
		t.Errorf("Claude 枠が失われた: %+v", snap.Windows)
	}
	if snap.HasCodex() {
		t.Error("codex 失敗なのに codex 枠がある")
	}
}

func TestFetchAllClaudeFailureKeepsCodex(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "claude", "exit 1\n")
	writeStub(t, dir, "codex", codexStubOK)
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap, err := FetchAll(ctx)
	if err != nil {
		t.Fatalf("claude 失敗でも codex 枠があれば成立するはず: %v", err)
	}
	if _, ok := snap.Find("cx7d"); !ok {
		t.Errorf("codex 枠がない: %+v", snap.Windows)
	}
}

func TestFetchAllBothFail(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "claude", "exit 1\n")
	writeStub(t, dir, "codex", "exit 1\n")
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := FetchAll(ctx); err == nil {
		t.Error("両方失敗でエラーにならなかった")
	}
}

func TestSnapshotHasClaude(t *testing.T) {
	tests := []struct {
		name string
		snap *Snapshot
		want bool
	}{
		{name: "claude-only", snap: &Snapshot{Windows: []Window{{Label: "5h"}}}, want: true},
		{name: "codex-only", snap: &Snapshot{Windows: []Window{{Label: "cx7d", Source: SourceCodex}}}, want: false},
		{name: "mixed", snap: &Snapshot{Windows: []Window{{Label: "5h"}, {Label: "cx7d", Source: SourceCodex}}}, want: true},
		{name: "empty", snap: &Snapshot{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.snap.HasClaude(); got != tt.want {
				t.Errorf("HasClaude() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeLastGood(t *testing.T) {
	prev := &Snapshot{Version: "2.1.216", Windows: []Window{
		{Label: "5h", Percent: 4},
		{Label: "7d", Percent: 29},
		{Label: "cx7d", Source: SourceCodex, Percent: 69},
	}}

	// claude だけ失敗した回 (codex 枠のみ・Version 空) → claude 枠とバージョンを引き継ぎ、
	// codex 枠は今回の新値が勝つ。
	got := &Snapshot{Windows: []Window{{Label: "cx7d", Source: SourceCodex, Percent: 73}}}
	got.MergeLastGood(prev)
	if len(got.Windows) != 3 {
		t.Fatalf("枠数 = %d, want 3: %+v", len(got.Windows), got.Windows)
	}
	if w, _ := got.Find("cx7d"); w.Percent != 73 {
		t.Errorf("codex 枠が前回値で上書きされた: %d, want 73", w.Percent)
	}
	if _, ok := got.Find("5h"); !ok {
		t.Errorf("claude 枠が引き継がれない: %+v", got.Windows)
	}
	if got.Version != "2.1.216" {
		t.Errorf("Version = %q, want 引き継ぎ 2.1.216", got.Version)
	}

	// codex だけ失敗した回 → codex 枠を引き継ぎ、Version は今回値が勝つ。
	got = &Snapshot{Version: "2.1.220", Windows: []Window{
		{Label: "5h", Percent: 10},
		{Label: "7d", Percent: 30},
	}}
	got.MergeLastGood(prev)
	if w, ok := got.Find("cx7d"); !ok || w.Percent != 69 {
		t.Errorf("codex 枠が引き継がれない: %+v", got.Windows)
	}
	if got.Version != "2.1.220" {
		t.Errorf("Version = %q, want 今回値 2.1.220", got.Version)
	}

	// 両方成功 → 引き継ぎなし (枠が重複しない)。prev=nil は no-op。
	got = &Snapshot{Version: "v", Windows: []Window{
		{Label: "5h"}, {Label: "7d"}, {Label: "cx7d", Source: SourceCodex},
	}}
	got.MergeLastGood(prev)
	if len(got.Windows) != 3 {
		t.Errorf("両方成功で枠が増えた: %+v", got.Windows)
	}
	got.MergeLastGood(nil)
	if len(got.Windows) != 3 {
		t.Errorf("prev=nil で枠が変わった: %+v", got.Windows)
	}
}

func TestRenderTableWithCodex(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "5h", Percent: 4, ResetAt: time.Date(2026, 7, 21, 16, 26, 0, 0, time.Local)},
		{Label: "7d", Percent: 29, ResetAt: time.Date(2026, 7, 26, 15, 0, 0, 0, time.Local)},
		{Label: "cx7d", Source: SourceCodex, Percent: 69, ResetAt: time.Date(2026, 8, 5, 13, 10, 0, 0, time.Local)},
	}}
	header, rows := RenderTable(snap, now, false)
	if len(rows) != 3 {
		t.Fatalf("行数 = %d, want 3", len(rows))
	}
	// codex 行は Claude 枠の後ろ。ラベル列は最長の "cx7d" (4) に広がる。
	if !strings.HasPrefix(rows[0], "5h     [") || !strings.HasPrefix(rows[2], "cx7d   [") {
		t.Errorf("ラベル列の幅/順序が想定外:\n%q\n%q", rows[0], rows[2])
	}
	// 列整列はラベル幅が広がっても保たれる (残り列の " / " とリセット時刻が縦に揃う)。
	for i := 1; i < len(rows); i++ {
		if a, b := colOf(t, rows[0], " / "), colOf(t, rows[i], " / "); a != b {
			t.Errorf("残り列の / 位置がずれる: %d vs %d\n%q\n%q", a, b, rows[0], rows[i])
		}
	}
	if a, b := colOf(t, header, " / "), colOf(t, rows[0], " / "); a != b {
		t.Errorf("ヘッダーの / 位置がデータ行とずれる: %d vs %d\n%q\n%q", a, b, header, rows[0])
	}
	if a, b := colOf(t, rows[0], "16:26"), colOf(t, rows[2], "13:10"); a != b {
		t.Errorf("リセット時刻の位置がずれる: %d vs %d\n%q\n%q", a, b, rows[0], rows[2])
	}
}

// RenderTableGroups は出所の境目で行を分け、RenderTable はその連結と一致する。
func TestRenderTableGroups(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "5h", Percent: 4, ResetAt: now.Add(4 * time.Hour)},
		{Label: "7d", Percent: 29, ResetAt: now.Add(50 * time.Hour)},
		{Label: "cx7d", Source: SourceCodex, Percent: 69, ResetAt: now.Add(120 * time.Hour)},
	}}
	_, groups := RenderTableGroups(snap, now, false)
	if len(groups) != 2 || len(groups[0]) != 2 || len(groups[1]) != 1 {
		t.Fatalf("グループ構成 = %v, want [2枠, 1枠]", groups)
	}
	if !strings.HasPrefix(groups[1][0], "cx7d") {
		t.Errorf("第 2 グループが codex でない: %q", groups[1][0])
	}
	_, rows := RenderTable(snap, now, false)
	if flat := append(append([]string{}, groups[0]...), groups[1]...); len(rows) != len(flat) ||
		rows[0] != flat[0] || rows[2] != flat[2] {
		t.Errorf("RenderTable がグループの連結と一致しない:\n%v\n%v", rows, flat)
	}

	// 単一出所 (codex なし) は 1 グループ。
	solo := &Snapshot{Windows: []Window{{Label: "5h", Percent: 4, ResetAt: now.Add(time.Hour)}}}
	if _, g := RenderTableGroups(solo, now, false); len(g) != 1 {
		t.Errorf("単一出所でグループ数 = %d, want 1", len(g))
	}
}

func TestRenderLineWithCodex(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "5h", Percent: 2, ResetAt: time.Date(2026, 7, 22, 3, 9, 0, 0, time.Local)},
		{Label: "cx7d", Source: SourceCodex, Percent: 69, ResetAt: time.Date(2026, 7, 24, 8, 0, 0, 0, time.Local)},
	}}
	got := RenderLine(snap, now, false)
	want := "5h:[▱▱▱▱▱▱▱▱▱▱]2%(残:15時間9分 / 7月22日03:09) cx7d:[▰▰▰▰▰▰▰▱▱▱]69%(残:2日20時間 / 7月24日08:00)"
	if got != want {
		t.Errorf("RenderLine:\n got=%q\nwant=%q", got, want)
	}
}

func TestParseCodexVersion(t *testing.T) {
	cases := map[string]string{
		"codex-cli 0.144.6":   "0.144.6",
		"codex-cli 0.144.6\n": "0.144.6",
		"0.144.6":             "0.144.6", // 素の semver だけになっても拾える
		"":                    "",
		"   \n":               "",
	}
	for in, want := range cases {
		if got := parseCodexVersion(in); got != want {
			t.Errorf("parseCodexVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// 未消費の枠は 1 行表示・表でも「未消費」と出し、ゼロ値のリセット時刻 (1月1日00:00 /
// リセット済み) を捏造しない。
func TestRenderUnusedWindow(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)
	snap := &Snapshot{Windows: []Window{
		{Label: "cx5h", Source: SourceCodex, Percent: 0, Unused: true, WindowMins: 300},
		{Label: "cx7d", Source: SourceCodex, Percent: 42, ResetAt: now.Add(48 * time.Hour), WindowMins: 10080},
	}}
	line := RenderLine(snap, now, false)
	if !strings.Contains(line, "cx5h:[▱▱▱▱▱▱▱▱▱▱]0%(未消費)") {
		t.Errorf("RenderLine = %q", line)
	}
	_, groups := RenderTableGroups(snap, now, false)
	all := strings.Join(groups[0], "\n")
	if !strings.Contains(all, "未消費") {
		t.Errorf("表に未消費が無い:\n%s", all)
	}
	for _, ng := range []string{"リセット済み", "1月1日"} {
		if strings.Contains(line+all, ng) {
			t.Errorf("未消費なのに %q を出している:\n%s\n%s", ng, line, all)
		}
	}
}
