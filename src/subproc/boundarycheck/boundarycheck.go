// Package boundarycheck は「外部コマンドの Cmd を作ったのに WaitDelay を張り忘れる」を止める検査の本体
// (glogx issue 105 / 303)。各 module のテストは Spec を渡して Run を呼ぶだけにする。
//
// なぜテストで縛るか (issue 105): WaitDelay の規律は glogx の 13 箇所中 12 箇所で守られ、1 箇所だけが静かに
// 抜けていた。抜けても手元でも CI でも何も起きず、孫プロセスがパイプを握って Wait が戻らなくなって初めて分かる
// 種類の欠落なので、**書く人が覚えているか**に依存させない。検査が module ごとに写されて (glogx / ratelimit /
// treefiler) 3 実装になり、pro-con と mac-lan-manager には写されもしなかったので、判定を 1 実装に寄せた (issue 777)。
//
// ■ 誰の・どの失敗を止めるか
//
//	module に**新しく外部コマンド実行を書いた人が、WaitDelay を張り忘れる**こと。止めたいのは**うっかり**であって、
//	意図的な迂回ではない。
//
// ■ 2 つの設定 (module ごとに Spec で選ぶ)
//
//	既定 (NoOsExec=false): 字句走査 + import 境界の 2 段構え。
//	  段 A = Cmd を作る行 (`exec.Command(` / `exec.CommandContext(`) の直後 lookahead 行以内に WaitDelay の代入が要る。
//	         例外は行内の注記 (MarkerNoWaitDelay / MarkerWaitDelayIn)。「既に os/exec を使っているファイルへの追記」
//	         (最頻の退行) と「WaitDelay の代入を消す」退行 (issue 105 の形) を止める。
//	  段 B = os/exec の別名 import を禁じ (段 A の字句の前提)、Cmd を作るファイルを Allow に固定する。
//	         「新しいファイルが Cmd を作り始める」を止める。段 B は WaitDelay を読まないので段 A の置き換えではない。
//	厳しい設定 (NoOsExec=true): 非テストの .go が os/exec を import しない。外部プロセスを subproc.CommandContext
//	  でしか起こさない module (ratelimit / treefiler) 用。起動口そのものを増やさない。
//
// ■ 検出しないと決めた形 (= 見つけても「穴」として塞ぎに行かない。review の責務)
//
//	(a) 字句を避ける書き方すべて: `exec.Cmd{Path: …}` の構造体リテラル / `syscall.Exec` / 別パッケージに薄いラッパを
//	    作ってそこから呼ぶ / 呼び出しを複数行に割って `exec.Command(` が 1 行に現れない形 / reflect 経由。
//	(b) 注記に**書かれた理由が真か**。注記の有無しか見ない。
//	(c) 代入が**同じ Cmd** に対して行われているか。判定は行単位の窓 (lookahead) で、窓は関数境界を跨ぐ。
//	(d) **新しい実行箇所を足すと同時に注記も足す**形。免除機構を持つ以上これは原理的に閉じられない。
//	    Spec の EnforcedFloor は「注記が増えて守りが消えた」の一部を拾う。
//	(e) 走査の根 (Spec.Root) の外。module ごとに呼ぶのはそのため。
//
// 塞ぎ続けると規則の軸が字句に固定され、迂回の指摘が無限に出て収束しない (adversarial-review-own-safeguards §8)。
package boundarycheck

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"subproc"
)

// 免除の注記。**2 種類あるのは意味が違うから**。片方で代用すると行内注記が嘘になる。
//
//	MarkerNoWaitDelay … WaitDelay が要らない (出力を取らない Run() / 前景で端末に直結)。理由を同じ行か直上に書く
//	MarkerWaitDelayIn … Cmd を受け取った呼び先が張る (glogx の gitlog.go → runGitCmd)
const (
	MarkerNoWaitDelay = "subproc: no-waitdelay"
	MarkerWaitDelayIn = "subproc: waitdelay-in"
)

// lookahead は実行箇所の直後に WaitDelay の代入を探す行数。
//
// 🚨 12 なのは glogx の gitlog.go の `return runGitCmd(exec.CommandContext(...))` が、代入を **呼び先の
// runGitCmd** (+9 行) で 1 度だけ張る形だから。つまりこの窓は**関数境界を跨ぐ**ので、
// 「同じ Cmd に張っている」ことまでは保証しない (脅威モデル (c))。
const lookahead = 12

// cmdPatterns は「Cmd を作っている」とみなす字句。
//
// 🚨 2 本必要。`exec.Command(` は `exec.CommandContext(` を**含まない** (前者はここで `(` を要求し、後者はその位置が `C`)。
// 片方だけにすると、もう片方の検査が丸ごと消える (issue 303 の A 案の本文がこの誤りを含んでいた)。
// subproc.CommandContext / subproc.Command は含まない (そちらが張る側)。
var cmdPatterns = []string{"exec.CommandContext(", "exec.Command("}

// goListTimeout は走査の突き合わせに使う go list の上限 (module の読み込みに依存を取りに行く形は想定しない)。
const goListTimeout = 2 * time.Minute

// Spec は 1 module ぶんの検査条件。
type Spec struct {
	// Root は走査の根。module の root で呼ぶテストからは "."。非テストの .go を 1 つずつ見る
	// (testdata / vendor / `.` `_` 始まりのディレクトリは Go のツールチェインと同じく常に飛ばす)
	Root string
	// SkipDirs は名前で飛ばすディレクトリ (本体から参照しない調査ツール。glogx の tools/)。
	SkipDirs []string
	// NoOsExec は厳しい設定: 非テストの .go が os/exec を import しない。true のとき Allow と Floor は使わない (0 のまま)
	NoOsExec bool
	// Allow は *exec.Cmd を作ってよいファイル (Root からの / 区切りの相対パス) → 理由。
	// 🚨 母集合は「os/exec を import しているファイル」ではなく「**Cmd を作るファイル**」。exec.ErrNotFound /
	// exec.ExitError を使うだけのファイルは載せない (載せると「Cmd を作っていないのに載っている」で落ちる)。
	// 増やすときは、そのファイルが本当に外部プロセスの起動口であるべきかを考える (subproc.CommandContext に寄せられないか)
	Allow map[string]string
	// SeenFloor / EnforcedFloor は字句走査の canary の下限。実件数のすぐ下に置く (0 は検査しない)。
	// seen = Cmd を作る行の数、enforced = そのうち注記で免除されなかった数。
	// **2 つを分けて数える**のは、注記を足すと enforced だけが減り、seen しか見ていない canary では
	// 「守りが薄くなった」が観測できないため (脅威モデル (d) の一部はここで拾える)。
	// 🚨 enforced に数えられている箇所に**注記を付けてはいけない**。ここを注記で逃がすと、
	// 「WaitDelay の代入を消す」退行 (issue 105 の形) を検出する箇所が 0 になる。正当に減らすときは、この定数も同じ commit で意識的に下げる
	SeenFloor, EnforcedFloor int
}

// report は check の結果。Run が go list との突き合わせと floor の判定に使う。
type report struct {
	problems       []string
	files          int            // 走査した非テストの .go
	perDir         map[string]int // Root からの相対ディレクトリ ("." が根) → 走査した .go の数
	seen, enforced int
}

// Run は 1 つの module について検査する。呼び出し側 (module の root の _test.go) が自分の設定を渡す。
//
// 走査が壊れて 0 件 = 緑に化けないよう、見たファイルの数は go list (別の方法の列挙) と**ディレクトリごとに**
// 突き合わせる (下限の定数は古くなる。treefiler issue 695 の 2)。go list が見ないディレクトリ (build tag で全部外れる
// gorules 等) を走査するのは構わない (走査が広い側は安全)。
func Run(t *testing.T, s Spec) {
	t.Helper()
	r, fatal := check(s)
	if fatal != "" {
		t.Fatal(fatal)
	}
	want, err := goListCounts(s)
	if err != nil {
		t.Fatalf("go list で走査を突き合わせられない: %v", err)
	}
	for _, dir := range sortedKeys(want) {
		if got := r.perDir[dir]; got != want[dir] {
			t.Fatalf("%s: 走査した非テストの .go が %d 件、go list では %d 件 (走査の根か除外が壊れている)", dir, got, want[dir])
		}
	}
	if s.SeenFloor > 0 && r.seen < s.SeenFloor {
		t.Fatalf("外部コマンド実行の検出数が %d 件しかない (下限 %d)。走査パスか検出パターンが壊れている疑い", r.seen, s.SeenFloor)
	}
	if s.EnforcedFloor > 0 && r.enforced < s.EnforcedFloor {
		t.Fatalf("WaitDelay を強制している箇所が %d 件しかない (下限 %d)。注記が増えて守りが消えていないか確認する", r.enforced, s.EnforcedFloor)
	}
	for _, p := range r.problems {
		t.Error(p)
	}
}

// check は検査の本体。**t を触らず、見つけた違反を文字列で返す** (検査器自身のテストが fixture で呼ぶ)。
// fatal は「検査できなかった」で、緑にしない。
func check(s Spec) (report, string) {
	r := report{perDir: map[string]int{}}
	if s.Root == "" {
		return r, "Spec.Root が空 (module の root で呼ぶなら \".\")"
	}
	if s.NoOsExec && (len(s.Allow) > 0 || s.SeenFloor != 0 || s.EnforcedFloor != 0) {
		return r, "NoOsExec のとき Allow / SeenFloor / EnforcedFloor は使わない (os/exec の import 自体を禁じるので Cmd を作るファイルは無い)"
	}
	observed := map[string]bool{}
	err := walk(s, func(rel string, src []byte) error {
		dir := filepath.ToSlash(filepath.Dir(rel))
		r.files++
		r.perDir[dir]++
		imp, err := osExecImport(rel, src)
		if err != nil {
			return err
		}
		if s.NoOsExec {
			if imp.found {
				r.problems = append(r.problems, rel+": os/exec を import している (外部プロセスは subproc.CommandContext で起動する)")
			}
			return nil
		}
		if imp.found && imp.alias != "" {
			r.problems = append(r.problems, rel+": os/exec を別名 import している ("+imp.alias+")。字句走査が素通りするので素の import にすること")
		}
		lines := strings.Split(string(src), "\n")
		offenders, seen, enforced := scanLines(rel, lines)
		r.problems = append(r.problems, offenders...)
		r.seen += seen
		r.enforced += enforced
		if seen == 0 {
			return nil
		}
		observed[rel] = true
		reason, ok := s.Allow[rel]
		switch {
		case !ok:
			r.problems = append(r.problems, rel+": *exec.Cmd を作っているが Allow に無い。subproc.CommandContext に寄せられないか検討し、"+
				"それでも要るなら理由つきで Allow へ足す")
		case strings.TrimSpace(reason) == "":
			r.problems = append(r.problems, rel+": Allow に理由が書かれていない (なぜ subproc.CommandContext に寄せないかを書く)")
		}
		return nil
	})
	if err != nil {
		return r, "ソース走査に失敗 (検査できないので緑にしない): " + err.Error()
	}
	if r.files == 0 {
		return r, "走査した非テストの .go が 0 件 (Root " + s.Root + " がずれている)"
	}
	// 逆向き: Allow に載っているのに Cmd を作らなくなったファイルを残さない
	// (放置すると「Cmd を作るファイル」でない項目が溜まり、母集合の意味が薄れる)
	for _, rel := range sortedKeys(s.Allow) {
		if !observed[rel] {
			r.problems = append(r.problems, rel+": Allow に載っているが Cmd を作っていない (走査対象から消えたか、外部実行をやめた)。Allow から外すこと")
		}
	}
	sort.Strings(r.problems)
	return r, ""
}

// walk は Root の下の非テストの .go を 1 つずつ fn へ渡す (rel は Root からの / 区切りの相対パス)。
func walk(s Spec, fn func(rel string, src []byte) error) error {
	skip := map[string]bool{"testdata": true, "vendor": true}
	for _, d := range s.SkipDirs {
		skip[d] = true
	}
	return filepath.WalkDir(s.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != s.Root && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), src)
	})
}

type importFact struct {
	found bool
	alias string // 別名 (`xe` / `_` / `.`)。素の import なら ""
}

// osExecImport は AST (import 節まで) で os/exec の import と別名を読む。字句で見ると書式 (1 行 import / グループ /
// コメント) に左右される。パースできないファイルは判定不能として失敗にする (緑にしない)。
func osExecImport(path string, src []byte) (importFact, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
	if err != nil {
		return importFact{}, fmt.Errorf("%s をパースできない: %w", path, err)
	}
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "os/exec" {
			fact := importFact{found: true}
			if imp.Name != nil {
				fact.alias = imp.Name.Name
			}
			return fact, nil
		}
	}
	return importFact{}, nil
}

// stripLineComment は行末コメントを落とす。
//
// 🚨 注記 (MarkerNoWaitDelay / MarkerWaitDelayIn) の判定には使わないこと。注記は行末コメントに書く運用なので、
// 剥がすと注記済みの行が offender に化ける。文字列リテラル中の `//` (URL 等) も切るが、切りすぎは
// 「代入が見つからない = 落ちる」側へ倒れる (静かに通る側には倒れない) ので許容する。
func stripLineComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// hasCmdCall は「コードとして Cmd を作っているか」を判定する。
//
// 🚨 コメントを剥がしてから見る。剥がさないと、`exec.Command(` と書いた doc コメントが幽霊の offender になるうえ
// **seen を水増しして floor の低下を隠す**。
func hasCmdCall(line string) bool {
	code := stripLineComment(line)
	for _, p := range cmdPatterns {
		if strings.Contains(code, p) {
			return true
		}
	}
	return false
}

// hasExemption は行内注記による免除を判定する。**生の行**を渡すこと。
func hasExemption(line string) bool {
	return strings.Contains(line, MarkerNoWaitDelay) || strings.Contains(line, MarkerWaitDelayIn)
}

// hasWaitDelayAssign は「コードとして WaitDelay を代入しているか」を判定する。
//
// 🚨 語の出現 (`strings.Contains(line, "WaitDelay")`) では駄目。実測 2026-09-09 (issue 303): glogx の gitlog.go の
// 唯一の強制対象は、+8 行目のコメント `(理由は subproc.WaitDelay の doc)` に引っかかって緑になっていた。
// **実ガード `cmd.WaitDelay = subproc.WaitDelay` を削除しても緑、そのコメントから語を消すと赤**という反転状態で、
// production の退行検出力は 0 だった。
func hasWaitDelayAssign(line string) bool {
	code := stripLineComment(line)
	return strings.Contains(code, "WaitDelay =") || strings.Contains(code, "WaitDelay:")
}

// scanLines は 1 ファイル分の行から段 A の違反を返す。走査本体と canary (テスト) の**両方がこの関数を通る**。
func scanLines(path string, lines []string) (offenders []string, seen, enforced int) {
	for i, line := range lines {
		if !hasCmdCall(line) {
			continue
		}
		seen++
		if hasExemption(line) { // 注記は生の行で見る (コメントを剥がすと注記そのものが消える)
			continue
		}
		enforced++
		found := false
		for j := i; j < len(lines) && j <= i+lookahead; j++ {
			if hasWaitDelayAssign(lines[j]) {
				found = true
				break
			}
		}
		if !found {
			offenders = append(offenders, path+":"+strconv.Itoa(i+1)+": WaitDelay を張っていない外部コマンド実行 (subproc.CommandContext を使うか、"+
				"理由つきで `"+MarkerNoWaitDelay+"` / `"+MarkerWaitDelayIn+"` を行内に書く): "+strings.TrimSpace(line))
		}
	}
	return offenders, seen, enforced
}

// goListCounts は go list が数える、Root の下のディレクトリごとの非テストの .go の数 (build tag で外れたものも含む)。
// SkipDirs の下は走査と同じく数えない。
func goListCounts(s Spec) (map[string]int, error) {
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return nil, err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), goListTimeout)
	defer cancel()
	cmd := subproc.CommandContext(ctx, "go", "list", "-e", "-json=Dir,GoFiles,CgoFiles,IgnoredGoFiles", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list (in %s): %w", root, err)
	}
	skip := map[string]bool{}
	for _, d := range s.SkipDirs {
		skip[d] = true
	}
	counts := map[string]int{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p struct {
			Dir                               string
			GoFiles, CgoFiles, IgnoredGoFiles []string
		}
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("go list の出力を読めない: %w", err)
		}
		dir, err := filepath.EvalSymlinks(p.Dir)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if underSkipped(rel, skip) {
			continue
		}
		n := 0
		for _, files := range [][]string{p.GoFiles, p.CgoFiles, p.IgnoredGoFiles} {
			for _, f := range files {
				if !strings.HasSuffix(f, "_test.go") { // IgnoredGoFiles には build tag で外れた _test.go も入る
					n++
				}
			}
		}
		counts[rel] += n
	}
	if len(counts) == 0 {
		return nil, fmt.Errorf("go list が package を 1 つも返さない (in %s)", root)
	}
	return counts, nil
}

func underSkipped(rel string, skip map[string]bool) bool {
	for _, e := range strings.Split(rel, "/") {
		if skip[e] {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
