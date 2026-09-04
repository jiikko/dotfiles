package disk

import (
	"strings"
	"testing"
	"time"
)

func ephEnv() Env  { return Env{Home: "/Users/x", TmpDir: "/var/folders/k2/abc/T/"} }
func keepEnv() Env { return Env{Home: "/Users/x", TmpDir: "/Users/x/tmp"} }

// 注記の判定は「Paths が $TMPDIR 起点か」と「**実効 TMPDIR が dirhelper の担当か**」の
// 両方を見る。文字列だけを見ると、TMPDIR を差し替えた環境で永久に残るものへ
// 「放置してよい」と表示する。
func TestRebootNoteNeedsBothTemplateAndEffectiveTmpDir(t *testing.T) {
	cache := Entry{ID: "c", Risk: RiskSafe, Paths: []string{"$TMPDIR/.com.google.Chrome.*"}}
	for _, tc := range []struct {
		name string
		e    Entry
		env  Env
		want string
	}{
		{"TMPDIR 配下 + 実効値も /var/folders", cache, ephEnv(), RebootClearsNote},
		{"TMPDIR そのもの", Entry{ID: "t", Paths: []string{"$TMPDIR"}}, ephEnv(), RebootClearsNote},
		{"実効 TMPDIR が HOME 配下 (dirhelper の外)", cache, keepEnv(), ""},
		{"TMPDIR が空", cache, Env{Home: "/Users/x"}, ""},
		{"/var/tmp は再起動で消えない", Entry{ID: "v", Paths: []string{"/private/var/tmp/x.*"}}, ephEnv(), ""},
		{"var/tmp を混ぜたら出さない", Entry{ID: "m", Paths: []string{"$TMPDIR/a", "/private/var/tmp/b"}}, ephEnv(), ""},
		{"HOME 配下", Entry{ID: "h", Paths: []string{"~/Library/Caches/electron"}}, ephEnv(), ""},
		{"Paths が空 (Guard だけのエントリ)", Entry{ID: "g"}, ephEnv(), ""},
		{"別の変数 ($TMPDIRX)", Entry{ID: "x", Paths: []string{"$TMPDIRX/a"}}, ephEnv(), ""},
		// 中身がユーザーファイルかもしれないものは含意が逆 (放置 = 失われる)
		{"Inspect は「失われる」側", Entry{ID: "i", Risk: RiskConfirm, Inspect: true, Paths: []string{"$TMPDIR/TemporaryItems/NSIRD_*"}}, ephEnv(), RebootLosesNote},
		{"RiskConfirm だけでも「失われる」側", Entry{ID: "r", Risk: RiskConfirm, Paths: []string{"$TMPDIR/a"}}, ephEnv(), RebootLosesNote},
	} {
		if got := RebootNote(tc.e, tc.env); got != tc.want {
			t.Errorf("%s: RebootNote()=%q, want %q", tc.name, got, tc.want)
		}
	}
}

// 既定カタログに対する期待値を **ID で名指しする** canary。
// 🚨 判定式をコピーして期待値を作らない (本走査と同じ式で期待値を作ると、受理集合を
// 変える変異が両側で同じに動いて red にならない。敵対レビュー 2026-09-04 の指摘)。
func TestCatalogRebootNotePerEntry(t *testing.T) {
	want := map[string]string{
		"chrome-tmp":   RebootClearsNote,
		"finder-nsird": RebootLosesNote, // 中身はユーザーファイルの可能性 (Inspect)
		// /private/var/tmp は dirhelper の対象外 = 再起動で消えない
		"xctest-logarchive":    "",
		"xctest-spindump":      "",
		"coresimulator-orphan": "",
		"launchd-tmp":          "",
		"speech-model-cache":   "",
		// HOME 配下のキャッシュ
		"npm-cache":          "",
		"npx-cache":          "",
		"go-build":           "",
		"deno-cache":         "",
		"swiftui-drag-cache": "",
		// Paths を持たない (Guard だけ) エントリ
		"simulator-runtimes":   "",
		"brew-cleanup-residue": "",
	}
	seen := 0
	for _, e := range catalog {
		w, ok := want[e.ID]
		if !ok {
			continue
		}
		seen++
		if got := RebootNote(e, ephEnv()); got != w {
			t.Errorf("%s: RebootNote()=%q, want %q", e.ID, got, w)
		}
	}
	// 抽出が空でも「違反 0 件」で緑になるのを塞ぐ。ID が改名されたらここで落ちる
	if seen != len(want) {
		t.Fatalf("期待値を書いた ID のうち %d/%d 件しかカタログに無い (改名された?)", seen, len(want))
	}
}

// 注記が **CLI レポートに実際に出る** ことまで見る (判定が正しいことと、呼び出し側が
// それを使っていることは別の主張)。
//
// 🚨 fixture は blocked と ok の**両方**を置く。カタログの $TMPDIR エントリは
// chrome-tmp (アプリ起動中 = blocked になりうる) と finder-nsird (候補が出れば ok) で、
// 片方だけの fixture だと「blocked のときだけ出す」ような退行を検出できない。
func TestFormatShowsRebootNoteForBothStatuses(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	blocked := Entry{ID: "b", Label: "揮発ブロック", Risk: RiskSafe, Recover: "再生成されます",
		DeleteVia: "rm", Paths: []string{"$TMPDIR/x-*"}}
	okEntry := Entry{ID: "o", Label: "揮発オーケー", Risk: RiskConfirm, Inspect: true, Recover: "元ファイルを確認",
		DeleteVia: "trash", Paths: []string{"$TMPDIR/y-*"}}
	failed := Entry{ID: "f", Label: "揮発フェイル", Risk: RiskSafe, Recover: "再生成されます",
		DeleteVia: "rm", Paths: []string{"$TMPDIR/z-*"}}
	keep := Entry{ID: "k", Label: "永続する置き場", Risk: RiskSafe, Recover: "再生成されません",
		DeleteVia: "rm", Paths: []string{"/private/var/tmp/w-*"}}
	rep := Report{Results: []Result{
		{Entry: blocked, Status: StatusBlocked, Reason: "アプリ起動中のため対象外"},
		{Entry: okEntry, Status: StatusOK, Size: 10, Items: []Item{{Path: "/tmpdir/y-1", Size: 10}}},
		{Entry: failed, Status: StatusFailed, Reason: "TMPDIR が空です"},
		{Entry: keep, Status: StatusOK, Size: 20, Items: []Item{{Path: "/private/var/tmp/w-1", Size: 20}}},
	}}
	out := Format(rep, ephEnv(), now)
	if n := strings.Count(out, RebootClearsNote); n != 1 {
		t.Errorf("「消える」注記の数が 1 でない (%d):\n%s", n, out)
	}
	if n := strings.Count(out, RebootLosesNote); n != 1 {
		t.Errorf("「失われる」注記の数が 1 でない (%d):\n%s", n, out)
	}
	// 走査できなかったエントリには出さない (場所が分かっていないので性質を言えない)
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if !strings.Contains(l, "揮発フェイル") {
			continue
		}
		for _, r := range lines[i+1 : min(i+3, len(lines))] {
			if strings.Contains(r, RebootClearsNote) || strings.Contains(r, RebootLosesNote) {
				t.Errorf("走査に失敗したエントリに注記を出した: %q", r)
			}
		}
	}
	// 位置: それぞれヘッダ行の直後 (助言の後ろに置くと blocked で continue して出ない)
	for _, pair := range [][2]string{{"揮発ブロック", RebootClearsNote}, {"揮発オーケー", RebootLosesNote}} {
		idx := -1
		for i, l := range lines {
			if strings.Contains(l, pair[0]) {
				idx = i
				break
			}
		}
		if idx < 0 || idx+1 >= len(lines) || !strings.Contains(lines[idx+1], pair[1]) {
			t.Errorf("%s の直後に注記が無い:\n%s", pair[0], out)
		}
	}
	// TMPDIR が dirhelper の外なら、同じ Report でも 1 件も出さない
	if out2 := Format(rep, keepEnv(), now); strings.Contains(out2, RebootClearsNote) || strings.Contains(out2, RebootLosesNote) {
		t.Errorf("TMPDIR が /var/folders の外なのに注記を出した:\n%s", out2)
	}
}
