package filer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runBang は `!` で cmd を打って Enter する (頼まれたプロセスは取り出して捨てる)。
func runBang(t *testing.T, m *Model, cmd string) {
	t.Helper()
	m.HandleKey("!")
	typeText(m, cmd)
	if m.HandleKey("enter") != Exec {
		t.Fatalf("%q の Enter で Exec を返さない", cmd)
	}
	m.TakeExec()
}

// upHistory は新しい入力欄で ↑ を n 回押した後の入力を返す。
func upHistory(m *Model, n int) string {
	m.HandleKey("!")
	for range n {
		m.HandleKey("up")
	}
	s := m.prompt.line.String()
	m.HandleKey("esc")
	return s
}

// 新しい Model (別のプロセス・別の dir の F) を作り直しても ↑ で前のコマンドが出る。同じ Model を閉じて開くだけのテストは
// 保存しなくても緑になるので、作り直す。
func TestBangHistorySurvivesNewModel(t *testing.T) {
	m := newTest(t)
	runBang(t, m, "echo one")
	runBang(t, m, "echo two")
	m2 := newAt(t, m.root.abs) // 同じ設定の dir (fixture が TREEFILER_CONFIG_DIR を張った) で作り直す
	if got := upHistory(m2, 1); got != "echo two" {
		t.Fatalf("作り直した Model の ↑ = %q", got)
	}
	if got := upHistory(m2, 2); got != "echo one" {
		t.Fatalf("作り直した Model の ↑↑ = %q", got)
	}
}

// 2 つの treefiler が交互に打っても、どちらの履歴も残る (後勝ちの全置換にしない)。
func TestBangHistoryKeepsBothOfTwoModels(t *testing.T) {
	a := newTest(t)
	b := newAt(t, a.root.abs)
	runBang(t, a, "cmd-a1")
	runBang(t, b, "cmd-b1")
	runBang(t, a, "cmd-a2")
	got, _ := readHistory(historyPath())
	if strings.Join(got, ",") != "cmd-a1,cmd-b1,cmd-a2" {
		t.Fatalf("履歴 = %q", got)
	}
}

func TestBangHistorySkipsLeadingSpaceAndIs0600(t *testing.T) {
	m := newTest(t)
	runBang(t, m, "echo keep")
	runBang(t, m, " echo secret")
	got, _ := readHistory(historyPath())
	if strings.Join(got, ",") != "echo keep" {
		t.Fatalf("先頭が空白のコマンドが残った: %q", got)
	}
	if upHistory(m, 1) != " echo secret" {
		t.Fatal("メモリの上の履歴からも消えた (今どおり残す)")
	}
	fi, err := os.Stat(historyPath())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("履歴ファイルの権限 = %v (%v)", fi.Mode().Perm(), err)
	}
}

// 上限の 2 倍を超えたファイルは、読むときに末尾 historyKeep 件で書き直す。改行を含むコマンドも 1 行で往復する。
func TestBangHistoryTrimsAndRoundTripsNewlines(t *testing.T) {
	isolateConfig(t)
	path := historyPath()
	for i := range 2*historyKeep + 50 {
		appendHistory(path, "c"+strconv.Itoa(i))
	}
	appendHistory(path, "printf 'a\nb'")
	m := newAt(t, t.TempDir())
	h := m.prompt.history
	if len(h) != historyKeep || h[len(h)-1] != "printf 'a\nb'" || h[0] != "c"+strconv.Itoa(2*historyKeep+50-historyKeep+1) {
		t.Fatalf("読んだ履歴 = %d 件 (先頭 %q・末尾 %q)", len(h), h[0], h[len(h)-1])
	}
	if _, lines := readHistory(path); lines != historyKeep {
		t.Fatalf("書き直した後の行数 = %d", lines)
	}
}

// 壊れた行 (引用になっていない・戻せない・長すぎる) は捨てて読み進める。落ちない。
func TestBangHistoryToleratesBrokenFile(t *testing.T) {
	newTest(t)
	path := historyPath()
	body := strconv.Quote("ok1") + "\n" + "not quoted\n" + `"unterminated` + "\n" + `"` + strings.Repeat("x", historyLineMax+10) + `"` + "\n" + strconv.Quote("ok2")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, lines := readHistory(path)
	if strings.Join(got, ",") != "ok1,ok2" || lines != 5 {
		t.Fatalf("履歴 = %q / 行数 = %d", got, lines)
	}
}

// writeHistoryLines は path に n 行 (c0, c1, …) の履歴を perm で書く。
func writeHistoryLines(t *testing.T, path string, n int, perm os.FileMode) {
	t.Helper()
	var b strings.Builder
	for i := range n {
		b.WriteString(strconv.Quote("c"+strconv.Itoa(i)) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
}

// 既にある 0644 のファイルも、足すたびに 0600 へ直す (OpenFile の mode は作るときにしか効かない)。
func TestBangHistoryFixesPermOfExistingFile(t *testing.T) {
	m := newTest(t)
	writeHistoryLines(t, historyPath(), 1, 0o644)
	runBang(t, m, "echo x")
	if fi, err := os.Stat(historyPath()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("権限 = %v (%v)", fi.Mode().Perm(), err)
	}
}

// 書き直すのは上限の 2 倍を「超えた」ときだけ。書き直したファイルも 0600。
func TestBangHistoryRewriteBoundaryAndPerm(t *testing.T) {
	newTest(t)
	path := historyPath()
	writeHistoryLines(t, path, 2*historyKeep, 0o644)
	newAt(t, t.TempDir())
	if _, lines := readHistory(path); lines != 2*historyKeep {
		t.Fatalf("ちょうど 2 倍で書き直した: %d 行", lines)
	}
	writeHistoryLines(t, path, 2*historyKeep+1, 0o644)
	newAt(t, t.TempDir())
	if _, lines := readHistory(path); lines != historyKeep {
		t.Fatalf("2 倍を超えたのに書き直さない: %d 行", lines)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("書き直したファイルの権限 = %v", fi.Mode().Perm())
	}
}

// 履歴のパスが symlink のとき、書き直しは link を普通のファイルに置き換え、link 先の中身は消さない
// (link 先は設定の dir の外の別のファイルかもしれない)。
func TestBangHistoryRewriteLeavesSymlinkTarget(t *testing.T) {
	newTest(t)
	path := historyPath()
	target := filepath.Join(t.TempDir(), "other-tool-history")
	writeHistoryLines(t, target, 2*historyKeep+1, 0o600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	newAt(t, t.TempDir())
	if _, lines := readHistory(target); lines != 2*historyKeep+1 {
		t.Fatalf("link 先を切り詰めた: %d 行", lines)
	}
	if _, lines := readHistory(path); lines != historyKeep {
		t.Fatalf("履歴を書き直していない: %d 行", lines)
	}
}

// 続けて同じコマンドは読むときに 1 件にする。他方が間に打った後に打ち直したコマンドはファイルに残り、次の ↑ に出る。
func TestBangHistoryCollapsesRunsAndKeepsRetyped(t *testing.T) {
	a := newTest(t)
	b := newAt(t, a.root.abs)
	runBang(t, a, "make")
	runBang(t, a, "make")
	runBang(t, b, "rm -rf build")
	runBang(t, a, "make")
	got, _ := readHistory(historyPath())
	if strings.Join(got, ",") != "make,rm -rf build,make" {
		t.Fatalf("履歴 = %q", got)
	}
	if up := upHistory(newAt(t, a.root.abs), 1); up != "make" {
		t.Fatalf("作り直した Model の ↑ = %q", up)
	}
}

// タブ・全角空白で始まるコマンドも残さない。
func TestBangHistorySkipsUnicodeLeadingSpace(t *testing.T) {
	newTest(t)
	path := historyPath()
	for _, c := range []string{"\techo tab", "　echo wide", " echo nbsp", "echo keep"} {
		appendHistory(path, c)
	}
	if got, _ := readHistory(path); strings.Join(got, ",") != "echo keep" {
		t.Fatalf("履歴 = %q", got)
	}
}

// 長すぎる行がファイルの最後 (改行なし) にあっても捨てる。
func TestBangHistoryDropsOverlongLastLine(t *testing.T) {
	newTest(t)
	path := historyPath()
	body := strconv.Quote("ok1") + "\n" + `"` + strings.Repeat("y", historyLineMax+10) + `"`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, lines := readHistory(path); strings.Join(got, ",") != "ok1" || lines != 2 {
		t.Fatalf("履歴 = %d 件 / 行数 = %d", len(got), lines)
	}
}

// 1 つの画面で打ち続けた `!` の履歴も、読み込みと同じ件数 (historyKeep) で頭から捨てる。
func TestPromptHistoryInMemoryIsCapped(t *testing.T) {
	m := newTest(t)
	for i := range historyKeep + 30 {
		m.HandleKey("!")
		typeText(m, "echo "+strconv.Itoa(i))
		m.HandleKey("enter")
		m.TakeExec()
	}
	h := m.prompt.history
	if len(h) != historyKeep || h[0] != "echo 30" || h[len(h)-1] != "echo "+strconv.Itoa(historyKeep+29) {
		t.Fatalf("履歴 %d 件 (先頭 %q・末尾 %q), want %d 件で古い方から捨てる", len(h), h[0], h[len(h)-1], historyKeep)
	}
}

// 履歴の場所が symlink なら追記しない (先のファイルに書かず、mode も変えない)。
func TestAppendHistoryDoesNotFollowSymlink(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "other")
	mustWrite(t, target, "keep\n")
	link := filepath.Join(d, "treefiler-history")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	appendHistory(link, "echo hi")
	b := readBody(t, target)
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if b != "keep\n" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("symlink の先へ書いた / mode を変えた: %q %v", b, fi.Mode().Perm())
	}
}
