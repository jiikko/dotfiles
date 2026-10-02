package usage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

func TestFetchCodexReturnsWhenDescendantHoldsStdout(t *testing.T) {
	// 子孫が stdout の書き込み端を握ったまま応答しない server。ctx が終わったら、子孫の終了を
	// 待たずに timeout として返ること (以前は子孫が終わるまで Scan が返らなかった)。
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "descendant.pid")
	writeStub(t, dir, "codex", "/bin/sleep 60 &\necho $! > "+pidFile+"\nexec /bin/sleep 60\n")
	t.Setenv("PATH", dir)
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := FetchCodex(ctx); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded を包んだエラー", err)
		}
	case <-time.After(30 * time.Second): // hang guard (子孫の sleep 60 より十分短い)
		t.Fatal("ctx が終わっても FetchCodex が返らない (子孫が stdout を握っている)")
	}
}

func TestFetchRunsClaudeOutsideCallerCwd(t *testing.T) {
	isolateShared(t)
	dir := t.TempDir()
	tmp := t.TempDir()
	pwdFile := filepath.Join(dir, "pwd")
	writeStub(t, dir, "claude", `case "$1" in --version) ;; *) pwd -P > `+pwdFile+`;; esac
`+claudeStubOK)
	t.Setenv("PATH", dir)
	t.Setenv("TMPDIR", tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Fetch(ctx); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(tmp)
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("claude -p の cwd = %q, want 一時ディレクトリ %q (呼び出し元の repo にセッション記録を作る)", strings.TrimSpace(string(got)), want)
	}
}

func TestFetchWorksWhenTempDirIsMissing(t *testing.T) {
	isolateShared(t)
	dir := t.TempDir()
	writeStub(t, dir, "claude", claudeStubOK)
	t.Setenv("PATH", dir)
	t.Setenv("TMPDIR", filepath.Join(dir, "missing")) // 存在しない一時ディレクトリ
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Fetch(ctx); err != nil {
		t.Fatalf("一時ディレクトリが無いだけで取得に失敗した: %v", err)
	}
}

// claudeStubOK は claude CLI の /usage と --version を模す (Fetch の期待する JSON 形)。
const claudeStubOK = `case "$1" in
--version) printf '%s\n' "9.9.9 (Claude Code)";;
*) printf '%s' '{"result":"Current session: 2% used ` + "·" + ` resets Jul 22 at 3:09am (Asia/Tokyo)\nCurrent week (all models): 29% used ` + "·" + ` resets Jul 24 at 8am (Asia/Tokyo)","is_error":false}';;
esac
`

func TestFetchPartsFromBothSources(t *testing.T) {
	isolateShared(t)
	dir := t.TempDir()
	writeStub(t, dir, "claude", claudeStubOK)
	writeStub(t, dir, "codex", "if [ \"$1\" = --version ]; then echo 'codex-cli 0.144.6'; exit 0; fi\n"+codexStubOK)
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, cx := FetchClaudePart(ctx), FetchCodexPart(ctx)
	if cl.Err != nil || cx.Err != nil {
		t.Fatalf("claude=%v codex=%v", cl.Err, cx.Err)
	}
	if cl.Source != "" || cx.Source != SourceCodex {
		t.Errorf("出所の印: claude=%q codex=%q", cl.Source, cx.Source)
	}
	snap := (*Snapshot)(nil).With(cl).With(cx)
	for _, label := range []string{"5h", "7d", "cx7d"} {
		if _, ok := snap.Find(label); !ok {
			t.Errorf("%s 枠がない: %+v", label, snap.Windows)
		}
	}
	if snap.Version != "9.9.9" || snap.CodexVersion != "0.144.6" || !snap.HasCodex() || snap.ClaudeErr != "" {
		t.Errorf("Version=%q CodexVersion=%q HasCodex=%v ClaudeErr=%q", snap.Version, snap.CodexVersion, snap.HasCodex(), snap.ClaudeErr)
	}
}

func TestFetchCodexPartFailure(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "codex", "exit 1\n")
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p := FetchCodexPart(ctx); p.Err == nil || len(p.Windows) != 0 || p.Source != SourceCodex {
		t.Errorf("codex 失敗の Part: %+v", p)
	}
}

func TestFetchClaudePartFailureKeepsReason(t *testing.T) {
	isolateShared(t)
	dir := t.TempDir()
	// PATH 上の壊れた shim (nodenv の別 node 版に入った claude 等) を模す: stderr に原因、rc=127
	// 端末制御列も混ぜる (理由は端末へ出るので落ちていること)
	writeStub(t, dir, "claude", "printf 'nodenv: \\033[31mclaude: command not found\\n' >&2\nexit 127\n")
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snap := (*Snapshot)(nil).With(FetchClaudePart(ctx))
	// 失敗を黙って捨てない: 理由 (exit status と stderr の最初の行) が呼び出し側へ届く
	for _, want := range []string{"exit status 127", "claude: command not found"} {
		if !strings.Contains(snap.ClaudeErr, want) {
			t.Errorf("ClaudeErr に %q が無い: %q", want, snap.ClaudeErr)
		}
	}
	if strings.Contains(snap.ClaudeErr, "\x1b") {
		t.Errorf("ClaudeErr に端末制御列が残っている: %q", snap.ClaudeErr)
	}
}

// 子の stderr は先頭だけ残す (上限なしにメモリへ溜めない)。書き込み自体は成功扱いにする。
func TestHeadWriterKeepsOnlyHead(t *testing.T) {
	w := &headWriter{max: 8}
	for _, chunk := range []string{"12345", "67890", "abc"} {
		if n, err := w.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	if got := w.buf.String(); got != "12345678" {
		t.Errorf("残った stderr = %q, want %q", got, "12345678")
	}
}

// 前回の失敗理由は引き継がない: Claude が取れた回で注記が消える (Version のような last-good にしない)。
func TestWithClaudeSuccessClearsClaudeErr(t *testing.T) {
	s := &Snapshot{ClaudeErr: "claude /usage 実行失敗: exit status 127"}
	got := s.With(Part{Windows: []Window{{Label: "5h"}}})
	if got.ClaudeErr != "" {
		t.Errorf("前回の ClaudeErr を引き継いだ: %q", got.ClaudeErr)
	}
	// codex の成功では消さない (Claude の値はまだ古いまま)
	if got := s.With(Part{Source: SourceCodex, Windows: []Window{{Label: "cx7d", Source: SourceCodex}}}); got.ClaudeErr == "" {
		t.Error("codex の成功で Claude の失敗理由が消えた")
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

func TestSnapshotWith(t *testing.T) {
	prev := &Snapshot{Version: "2.1.216", CodexVersion: "0.1", Windows: []Window{
		{Label: "5h", Percent: 4},
		{Label: "7d", Percent: 29},
		{Label: "cx7d", Source: SourceCodex, Percent: 69},
	}}
	labels := func(s *Snapshot) string {
		var ls []string
		for _, w := range s.Windows {
			ls = append(ls, fmt.Sprintf("%s=%d", w.Label, w.Percent))
		}
		return strings.Join(ls, ",")
	}

	// claude だけ失敗 → claude 枠とバージョンは前回のまま、理由が載る。codex 枠は新値。
	got := prev.With(Part{Err: errors.New("boom")}).
		With(Part{Source: SourceCodex, Version: "0.2", Windows: []Window{{Label: "cx7d", Source: SourceCodex, Percent: 73}}})
	if l := labels(got); l != "5h=4,7d=29,cx7d=73" {
		t.Errorf("枠 = %s, want 5h=4,7d=29,cx7d=73", l)
	}
	if got.Version != "2.1.216" || got.CodexVersion != "0.2" || got.ClaudeErr != "boom" {
		t.Errorf("Version=%q CodexVersion=%q ClaudeErr=%q", got.Version, got.CodexVersion, got.ClaudeErr)
	}

	// Claude が取れた後に codex が失敗しても、codex の理由を Claude の失敗として載せない
	if got := prev.With(Part{Windows: []Window{{Label: "5h"}}}).With(Part{Source: SourceCodex, Err: errors.New("codex 起動失敗")}); got.ClaudeErr != "" {
		t.Errorf("codex の失敗が ClaudeErr に載った: %q", got.ClaudeErr)
	}

	// codex だけ失敗 → codex 枠は前回のまま (黙る)、claude は新値。
	got = prev.With(Part{Source: SourceCodex, Err: errors.New("x")}).
		With(Part{Version: "2.1.220", Windows: []Window{{Label: "5h", Percent: 10}, {Label: "7d", Percent: 30}}})
	if l := labels(got); l != "5h=10,7d=30,cx7d=69" {
		t.Errorf("枠 = %s", l)
	}
	if got.Version != "2.1.220" || got.ClaudeErr != "" {
		t.Errorf("Version=%q ClaudeErr=%q", got.Version, got.ClaudeErr)
	}

	// 届いた順に依らず Claude が先に並ぶ (codex が先に届いた初回)
	got = (*Snapshot)(nil).With(Part{Source: SourceCodex, Windows: []Window{{Label: "cx7d", Source: SourceCodex, Percent: 1}}}).
		With(Part{Windows: []Window{{Label: "5h", Percent: 2}, {Label: "7d", Percent: 3}}})
	if l := labels(got); l != "5h=2,7d=3,cx7d=1" {
		t.Errorf("枠の並び = %s, want Claude が先", l)
	}

	// 元の Snapshot を書き換えない (glogx はポインタを描画キャッシュの鍵にする)
	if l := labels(prev); l != "5h=4,7d=29,cx7d=69" || prev.Version != "2.1.216" || prev.ClaudeErr != "" {
		t.Errorf("With が元を書き換えた: %s %q %q", l, prev.Version, prev.ClaudeErr)
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
