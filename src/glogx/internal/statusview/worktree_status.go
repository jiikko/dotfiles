package statusview

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"glogx/internal/app"
	"glogx/internal/gitdata"
	"glogx/internal/gitexec"
	"glogx/internal/term"
	"glogx/issues"
)

// 作業ツリーの状態 (git status) の読み取りとセクション分類。status viewer (status_view.go) が
// 表示に使う「派生ビューの素」を作る層で、画面・キー操作は一切知らない。
//
// 読む規約の一次情報は docs/status-viewer-spec.md。要点だけ再掲すると:
//
//   - v1 (--porcelain) を使う。v2 は 1 行のフィールド数が状態で変わる一方、この画面が要るのは
//     XY とパスだけなので「安定を git 自身が約束している最小の形式」を選ぶ
//   - -z (NUL 区切り) は必須。既定の LF 区切りは空白・日本語・引用符を含むパスを "..." で
//     クォートするため、素朴に分割するとパスがずれる
//   - untracked は既定 (normal) のまま = ディレクトリは "dir/" に畳まれて 1 行になる

// worktreeSection は 1 エントリが属する区画。表示順もこの順 (spec 2 節)。
type worktreeSection int

const (
	sectionStaged worktreeSection = iota
	sectionUnstaged
	sectionUntracked
	sectionConflicted
)

// worktreeRow は一覧の 1 行。同じパスが staged と unstaged の両方に出ることがある
// (X と Y の両方が立つ "MM" 等) ので、行の同一性は (section, path) で決まる。
type worktreeRow struct {
	section worktreeSection
	// code は表示する 1 文字 (M/A/D/R/C/?/U)。section 側の向きに対応する側を取る:
	// Staged 行なら X、Unstaged 行なら Y。
	code byte
	// x / y は git の XY をそのまま持つ (実行時の状態再検証に使う。spec 4 節の不変条件 1)。
	x, y byte
	path string
	// orig は rename 元 ("" = rename でない)。git へ操作を渡すときは orig も一緒に渡す。
	orig string
	// partial は「index 側と作業ツリー側の両方に変更がある」= 一部だけ staged (◐ を出す)。
	partial bool
}

// dispPath は画面・通知に出す用のパス (rename は "元 → 先")。
//
// 🚨 git へ渡すのは必ず path / orig / pathspecs() の生の方。表示用と同一性を分けているのは、
// 無害化した文字列で git を叩くとファイルを見失う (最悪、別のファイルを操作する) ため。
//
// なぜ無害化が要るか: POSIX のファイル名は / と NUL 以外の任意バイトを許し、-z の git status は
// クォートせず生のまま返す。第三者ブランチに ESC 入りの名前のファイルが 1 つあるだけで、
// status viewer を開いた瞬間に端末のタイトル書き換え・画面破壊・OSC52 が発火しうる。
//
// 🚨 既知の限界: 無害化は「落とす」方式なので単射でない。制御文字だけが違う 2 ファイル
// (notes.txt と notes<0x01>.txt) は同じ行に見え、この画面は破壊的操作 (X で変更を捨てる /
// untracked の削除) を持つため「区別できないまま捨てる」余地が残る。エスケープシーケンスに
// よる衝突は導入子だけ落とす形にして解消済み (termsafe.skipEscape) で、残るのは単独の制御
// 文字だけ。直すなら削除でなく可視のプレースホルダ (␛ 等) への置換に変える。実害の報告が
// 出たか、ファイル名に制御文字を使う運用を実際に見たら着手する。
func (r worktreeRow) dispPath() string {
	if r.orig != "" {
		return term.SanitizePlainLine(r.orig) + " → " + term.SanitizePlainLine(r.path)
	}
	return term.SanitizePlainLine(r.path)
}

// worktreeStatus は git status 1 回分の読み取り結果 (画面が毎回作り直す派生ビューの素)。
type worktreeStatus struct {
	rows []worktreeRow
	// branch / track はヘッダー行 (`--branch` の "## " レコード) 由来。track は "ahead 1" のような
	// 追跡状況 ("" = upstream なし / 同期済み)。ここで一緒に取るのは、ブランチ名のために
	// git を 2 回起動しないため。
	branch string
	track  string
	// root は repo root の絶対パス ("" = 取得失敗)。rows のパスは root 相対なので、
	// ファイルを直接読む経路 (untracked のプレビュー) はここと結合して絶対パスにする。
	root string
	// skipped は解釈できずに捨てたレコード数。🚨 画面に出すために数える: 捨てるだけだと
	// 「git の出力が読めなかった」が「変更なし = クリーン」と同じ絵になり、変更を見せるための
	// 画面が黙って嘘をつく (沈黙を成功にしない)。
	skipped int
}

// conflictCodes は「両者が触った」= 操作させない XY の組 (git status の man の unmerged 一覧)。
// 🚨 AA / DD のように X と Y が同じ文字でも conflict なので、片側だけ見て判定してはならない。
var conflictCodes = map[string]bool{
	"DD": true, "AU": true, "UD": true, "UA": true, "DU": true, "AA": true, "UU": true,
}

// parseWorktreeStatus は `git status --porcelain -z` の出力を行へ分解する純関数。
//
// -z のレコードは "XY <path>\x00" で、rename/copy だけ "XY <new>\x00<old>\x00" と 2 つ使う。
// 壊れたレコード (2 文字未満・パスなし) は黙って捨てる: status の読み取りに失敗して画面が
// 出ないより、読めた分だけ出す方が有用 (glogx 全体の「壊れない」方針)。
func parseWorktreeStatus(out string) worktreeStatus {
	fields := strings.Split(out, "\x00")
	var st worktreeStatus
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if strings.HasPrefix(rec, "## ") {
			st.branch, st.track = parseBranchHeader(rec)
			continue
		}
		if rec == "" {
			continue // -z の末尾 NUL による空要素 (異常ではないので数えない)
		}
		if len(rec) < 4 || rec[2] != ' ' { // "XY path" で最短 4 文字 ("M  a" 相当)
			st.skipped++
			continue
		}
		x, y := rec[0], rec[1]
		path := rec[3:]
		var orig string
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			// rename/copy は次のレコードが元のパス。無ければ (壊れた出力) orig なしで続ける
			if i+1 < len(fields) && fields[i+1] != "" {
				orig = fields[i+1]
				i++
			}
		}
		st.rows = append(st.rows, rowsFor(x, y, path, orig)...)
	}
	return st
}

// parseBranchHeader は `## master...origin/master [ahead 1]` 形式のヘッダーを
// (ブランチ名, 追跡状況) へ分ける純関数。
//
// 形は 3 通りある: upstream つき / upstream なし (`## master`) / detached (`## HEAD (no branch)`)。
// detached はブランチ名として "HEAD (no branch)" をそのまま出す (加工して「不明」と書くより、
// git が言っている文言をそのまま見せた方が誤解が無い)。
func parseBranchHeader(rec string) (branch, track string) {
	rest := strings.TrimPrefix(rec, "## ")
	if i := strings.Index(rest, " ["); i >= 0 {
		track = strings.Trim(rest[i+1:], "[]")
		rest = rest[:i]
	}
	if i := strings.Index(rest, "..."); i >= 0 {
		rest = rest[:i]
	}
	// ref 名は ASCII の制御文字しか禁じられておらず、C1 や双方向テキストの制御文字を含む
	// ブランチは正当に作れる。ヘッダの表示にしか使わないので、ここで平文にする
	return term.SanitizePlainLine(rest), term.SanitizePlainLine(track)
}

// rowsFor は 1 エントリを表示行へ写像する (spec 2 節の表)。XY の両方が立つ場合だけ 2 行になる。
func rowsFor(x, y byte, path, orig string) []worktreeRow {
	if conflictCodes[string([]byte{x, y})] {
		return []worktreeRow{{section: sectionConflicted, code: 'U', x: x, y: y, path: path, orig: orig}}
	}
	if x == '?' && y == '?' {
		return []worktreeRow{{section: sectionUntracked, code: '?', x: x, y: y, path: path}}
	}
	staged, unstaged := x != ' ', y != ' '
	partial := staged && unstaged
	var rows []worktreeRow
	if staged {
		rows = append(rows, worktreeRow{section: sectionStaged, code: x, x: x, y: y, path: path, orig: orig, partial: partial})
	}
	if unstaged {
		rows = append(rows, worktreeRow{section: sectionUnstaged, code: y, x: x, y: y, path: path, orig: orig, partial: partial})
	}
	return rows
}

// loadWorktreeStatus はテストで実 git を叩かないための差し替え点。
var loadWorktreeStatus = func() (worktreeStatus, error) {
	// TUI の tea.Cmd (goroutine) から呼ぶので timeout 付き (runGitTimeout の doc 参照)。
	// --ignore-submodules=dirty: submodule の中の変更を見に子の git を起こさない (子は submodule 自身の設定の filter を読む)
	out, err := gitexec.RunGitReadTimeout("status", "--porcelain", "--branch", "-z", "--ignore-submodules=dirty")
	if err != nil {
		return worktreeStatus{}, err
	}
	st := parseWorktreeStatus(out)
	// root は失敗しても致命ではない (untracked のプレビューだけが cwd 相対に落ちる) ので
	// エラーを伝播させない。git 操作側は pathspec の :(top) で cwd 非依存になっている。
	// 🚨 **解決できなかったら空のままにする (cwd で埋めない)**。空は「root が不明」の印で、
	// untracked のプレビューが cwd 相対に落ちるだけで済む。cwd を入れると
	// **間違った root を本物として扱う** (サブディレクトリで実行していれば別の場所を指す)。
	// この違いが出るのは「`git status` は成功するが `rev-parse` だけ失敗する」ときだけなので
	// テストでは固定できない (seam の新設が要る)。
	if root, ok := issues.ResolveRepoRoot(app.CurrentDir()); ok {
		st.root = root
	}
	return st, nil
}

// loadWorktreeDiff はカーソル行の diff を取る差し替え点。staged なら index との差分
// (--cached)、それ以外は作業ツリーとの差分。untracked は git diff の対象外なのでファイルの
// 中身をそのまま出す (呼び出し側で分岐)。
var loadWorktreeDiff = func(paths []string, staged, colored bool) ([]string, error) {
	// repo の設定のコマンドを差分の表示で走らせない (subproc.GitArgs / GitRepoArgs の doc)
	args := []string{"diff", "--no-textconv", "--no-ext-diff", "--ignore-submodules=dirty"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--color=never", "--")
	args = append(args, paths...)
	out, err := gitexec.RunGitReadTimeout(args...)
	if err != nil {
		return nil, err
	}
	return gitdata.DiffDisplayLines(out, colored, "... (これ以降は省略)"), nil
}

// counts はヘッダー/セクション見出し用の件数 (staged, unstaged, untracked, conflicted)。
func (s worktreeStatus) counts() (staged, unstaged, untracked, conflicted int) {
	for _, r := range s.rows {
		switch r.section {
		case sectionStaged:
			staged++
		case sectionUnstaged:
			unstaged++
		case sectionUntracked:
			untracked++
		case sectionConflicted:
			conflicted++
		}
	}
	return
}

// clean は変更が何も無いか (画面は開くが「クリーンです」を出す。spec 6 節)。
func (s worktreeStatus) clean() bool { return len(s.rows) == 0 }

// section は section 内の行だけを表示順で返す。
func (s worktreeStatus) section(sec worktreeSection) []worktreeRow {
	var out []worktreeRow
	for _, r := range s.rows {
		if r.section == sec {
			out = append(out, r)
		}
	}
	return out
}

// ordered は表示順 (Staged → Unstaged → Untracked → Conflicted) に並べた全行。
// git status の出力順はセクション内の並びとしてそのまま尊重する (パスでソートし直さない:
// git が見せている順と画面が食い違うと「git status で見た並び」との対応が崩れる)。
func (s worktreeStatus) ordered() []worktreeRow {
	out := make([]worktreeRow, 0, len(s.rows))
	for _, sec := range []worktreeSection{sectionStaged, sectionUnstaged, sectionUntracked, sectionConflicted} {
		out = append(out, s.section(sec)...)
	}
	return out
}

// find は (section, path) で行を引く。実行時の状態再検証 (spec 4 節) とカーソルのパス固定に使う。
func (s worktreeStatus) find(sec worktreeSection, path string) (worktreeRow, bool) {
	for _, r := range s.rows {
		if r.section == sec && r.path == path {
			return r, true
		}
	}
	return worktreeRow{}, false
}

// label はセクション見出しの語 (件数は呼び出し側が付ける)。
func (sec worktreeSection) label() string {
	switch sec {
	case sectionStaged:
		return "Staged"
	case sectionUnstaged:
		return "Unstaged"
	case sectionUntracked:
		return "Untracked"
	default:
		return "Conflicted"
	}
}

// mutable は Space / a / X を受け付けるセクションか。Conflicted だけ操作させない
// (conflict の解決はシェルの仕事。spec 3 節)。
func (sec worktreeSection) mutable() bool { return sec != sectionConflicted }

// isDir は untracked のディレクトリ行 ("dir/" に畳まれたもの) か。破壊操作の確認文言が
// 「ファイルを削除」ではなく「ディレクトリを中身ごと削除」になる (spec 4 節)。
func (r worktreeRow) isDir() bool {
	return r.section == sectionUntracked && strings.HasSuffix(r.path, "/")
}

// paths は対象のパス列 (rename は先と元の両方)。表示・通知に使う生のパス。
func (r worktreeRow) paths() []string {
	if r.orig != "" {
		return []string{r.path, r.orig}
	}
	return []string{r.path}
}

// pathspecs は git へ渡す形のパス列。
//
// 🚨 `:(top,literal)` を必ず付ける。両方に理由がある:
//
//   - **top**: `git status` が返すパスは repo root 相対だが、pathspec は **cwd 相対**に
//     解釈される。glogx は tmux popup から任意のサブディレクトリで起動される
//     (`-d '#{pane_current_path}'`) ため、素のパスを渡すと別の場所を指す。実測 2026-08-03:
//     `src/glogx` を cwd にして走らせると diff が常に空になり、`add` は "did not match any
//     files" で失敗し、`clean` は黙って何も消さない (= 操作が無言で効かない最悪の形)
//   - **literal**: ファイル名に含まれる `*` `?` `[` を glob として解釈させない
//     (`a?.txt` を消したいのに `ab.txt` まで巻き込む事故を防ぐ)
func (r worktreeRow) pathspecs() []string {
	out := make([]string, 0, 2)
	for _, p := range r.paths() {
		out = append(out, worktreePathspec(p))
	}
	return out
}

// worktreePathspecs は作業ツリーを書き戻すときに git へ渡すパス列。rename 元は作業ツリー側の rename (Y が R / C) のときだけ渡す:
// staged の rename (RM) の元は index にもう無く、restore が pathspec の不一致で全体を止める (作業ツリーの変更は rename 先にしか無い)。
func (r worktreeRow) worktreePathspecs() []string {
	if r.orig != "" && (r.y == 'R' || r.y == 'C') {
		return r.pathspecs()
	}
	return []string{worktreePathspec(r.path)}
}

// worktreePathspec は root 相対パスを cwd 非依存・glob 無効の pathspec にする (理由は pathspecs)。
func worktreePathspec(path string) string { return ":(top,literal)" + path }

// rowPrintMaxEntries は未追跡のディレクトリ行の指紋で見るファイルの上限。超えたら指紋を作らない (errPrintTooMany)。
// 上限で打ち切って「先頭の N 件が同じなら同じ」と言うと、N 件より後ろの変化を見落としたまま捨てる。
const rowPrintMaxEntries = 20000

// rowPrintMaxDuration は未追跡のディレクトリ行の指紋の時間の上限 (キー処理の中で同期に採る。stat の遅いマウントで UI を止めない)。
// var なのはテストが 0 にして上限を越えさせるため。
var rowPrintMaxDuration = time.Second

var (
	errPrintTooMany    = fmt.Errorf("中のファイルが %d 件を超えるため、変化を確かめられません", rowPrintMaxEntries)
	errPrintTooSlow    = errors.New("中のファイルを調べるのに時間がかかりすぎるため、変化を確かめられません")
	errPrintTrackedDir = errors.New("作業ツリーではディレクトリになっていて、中ごと消すことになるため、変化を確かめられません")
)

// readIndexEntries / listUntrackedFiles は指紋が叩く git (テストの差し替え点)。--full-name: 出力を cwd ではなく repo root
// 相対にする (glogx はサブディレクトリから起動されうる。pathspec の :(top) は入力の解釈しか変えない)。
var (
	readIndexEntries = func(pathspecs []string) (string, error) {
		return gitexec.RunGitReadTimeout(append([]string{"ls-files", "-s", "-z", "--full-name", "--"}, pathspecs...)...)
	}
	listUntrackedFiles = func(pathspec string) (string, error) {
		return gitexec.RunGitReadTimeout("ls-files", "-o", "--exclude-standard", "-z", "--full-name", "--", pathspec)
	}
)

// rowPrint は行の中身の指紋 (同じなら、プレビュー・確認で見せたものから変わっていない、と言える値)。X の確認と y の実行の間・
// プレビューと X の間で、XY は同じまま中身だけが変わったことを見つける (status_view.go の discardPrint / seenPrints)。
//
//   - index 側: `git ls-files -s` の行 (mode・blob・stage)。` M` の restore は index の中身を作業ツリーへ書くので、
//     index だけが変わる (MM の行で別プロセスが stage し直す) と、作業ツリーの指紋が同じでも書き戻す中身が変わる
//   - 作業ツリー側: statPrint。未追跡のディレクトリ行は git clean が消す集合 (ignore を除く未追跡のファイル) の各 statPrint
//
// 中身を読んで hash しない: X / y のキー処理の中で同期に採るので、大きなファイルで UI が止まる (4 GiB で 2 秒)。
// 書き直せば stat は変わる (APFS で同じ 1 byte を書き直して 5000 回中 5000 回変わった。mtime の差は最小 21µs)。
//
// 見ないもの (どれも見落としではなく、安全側か被害が無い): 未追跡のディレクトリに増えた空のディレクトリ (clean が消すが
// 中身が無い)・入れ子の repo の中の変化 (stat が変わって断るが、clean -fd は入れ子の repo を消さない = 断り過ぎ)。
// 共有の mmap で書いて msync する前の変化は時刻が遅れて付きうる (POSIX が許す) が、macOS で反例は作れていない
// (再提起するなら、その書き方で stat が変わらない実測を添える)
func rowPrint(root string, row worktreeRow) (string, error) {
	var b strings.Builder
	if !row.isDir() {
		if row.section != sectionUntracked {
			// 🚨 tracked のパスが作業ツリーではディレクトリ (submodule・ファイルをディレクトリに置き換えた) なら採らない:
			// restore は中を丸ごと消して書き戻す (submodule.recurse なら submodule の中の変更も) のに、指紋はディレクトリ自身の
			// stat しか見ないので、中のファイルの変化を見落とす
			if fi, err := os.Lstat(filepath.Join(root, row.path)); err == nil && fi.IsDir() {
				return "", errPrintTrackedDir
			}
			idx, err := readIndexEntries(row.pathspecs())
			if err != nil {
				return "", err
			}
			b.WriteString("index:" + idx + "\n")
		}
		for _, p := range row.paths() {
			b.WriteString(statPrint(filepath.Join(root, p)) + "\n")
		}
		return b.String(), nil
	}
	deadline := time.Now().Add(rowPrintMaxDuration) // 一覧を取る時間も含める
	out, err := listUntrackedFiles(worktreePathspec(row.path))
	if err != nil {
		return "", err
	}
	var names []string // 空の出力を Split すると [""] になり、root そのものの stat (mtime は中の何が変わっても動く) を数える
	if out != "" {
		names = strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	}
	if len(names) > rowPrintMaxEntries {
		return "", errPrintTooMany
	}
	if time.Now().After(deadline) { // 一覧を取るだけで越えた (中が空でも)
		return "", errPrintTooSlow
	}
	for _, name := range names {
		// 名前は引用する: 改行を含む名前・リンク先で、別のファイルの集合と同じ文字列にならないように
		b.WriteString(strconv.Quote(name) + " " + statOf(filepath.Join(root, name)) + "\n")
		if time.Now().After(deadline) { // stat の後に見る (最後の 1 件の stat で越えた分も数える)
			return "", errPrintTooSlow
		}
	}
	return b.String(), nil
}

// statOf は未追跡のディレクトリ行の各ファイルの指紋 (テストが遅い stat を差し込む口)。
var statOf = statPrint

// statPrint は 1 つのパスの作業ツリー側の指紋。中身は読まない (rowPrint の doc)。
//
//   - mode・inode・大きさ・mtime・ctime (ctime は書いた側が戻せない)。symlink は加えてリンク先の文字列 (辿らない。引用する)
//   - 無い: "absent"。読めない: "error:<理由>" (読めない状態が続けば一致する。読めるようになれば食い違って中止に倒れる)
func statPrint(path string) string {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent"
	}
	if err != nil {
		return "error:" + err.Error()
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "error:stat を読めません"
	}
	fp := fmt.Sprintf("%v %d %d %d %d", fi.Mode(), st.Ino, fi.Size(), st.Mtimespec.Nano(), st.Ctimespec.Nano())
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "error:" + err.Error()
		}
		return fp + " link:" + strconv.Quote(target)
	}
	return fp
}
