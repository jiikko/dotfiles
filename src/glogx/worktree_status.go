package main

import (
	"strings"
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
// ⚠️ git へ渡すのは必ず path / orig / pathspecs() の生の方。表示用と同一性を分けているのは、
// 無害化した文字列で git を叩くとファイルを見失う (最悪、別のファイルを操作する) ため。
//
// なぜ無害化が要るか: POSIX のファイル名は / と NUL 以外の任意バイトを許し、-z の git status は
// クォートせず生のまま返す。第三者ブランチに ESC 入りの名前のファイルが 1 つあるだけで、
// status viewer を開いた瞬間に端末のタイトル書き換え・画面破壊・OSC52 が発火しうる。
func (r worktreeRow) dispPath() string {
	if r.orig != "" {
		return sanitizePlainLine(r.orig) + " → " + sanitizePlainLine(r.path)
	}
	return sanitizePlainLine(r.path)
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
	// skipped は解釈できずに捨てたレコード数。⚠️ 画面に出すために数える: 捨てるだけだと
	// 「git の出力が読めなかった」が「変更なし = クリーン」と同じ絵になり、変更を見せるための
	// 画面が黙って嘘をつく (沈黙を成功にしない)。
	skipped int
}

// conflictCodes は「両者が触った」= 操作させない XY の組 (git status の man の unmerged 一覧)。
// ⚠️ AA / DD のように X と Y が同じ文字でも conflict なので、片側だけ見て判定してはならない。
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
	return rest, track
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
	out, err := runGitTimeout("status", "--porcelain", "--branch", "-z")
	if err != nil {
		return worktreeStatus{}, err
	}
	st := parseWorktreeStatus(out)
	// root は失敗しても致命ではない (untracked のプレビューだけが cwd 相対に落ちる) ので
	// エラーを伝播させない。git 操作側は pathspec の :(top) で cwd 非依存になっている。
	if root, rootErr := runGitTimeout("rev-parse", "--show-toplevel"); rootErr == nil {
		st.root = strings.TrimSpace(root)
	}
	return st, nil
}

// loadWorktreeDiff はカーソル行の diff を取る差し替え点。staged なら index との差分
// (--cached)、それ以外は作業ツリーとの差分。untracked は git diff の対象外なのでファイルの
// 中身をそのまま出す (呼び出し側で分岐)。
var loadWorktreeDiff = func(paths []string, staged, colored bool) ([]string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--color=never", "--")
	args = append(args, paths...)
	out, err := runGitTimeout(args...)
	if err != nil {
		return nil, err
	}
	return diffLines(out, colored), nil
}

// diffLines は git diff の生出力を表示行へ整える (行数上限・幅の正規化・色付け)。
// LoadCommitDiff と同じ整形なので、揃えるためにここも sanitizeDetailLine + HighlightDiff を通す。
func diffLines(out string, colored bool) []string {
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return nil
	}
	lines := make([]string, 0, min(strings.Count(trimmed, "\n")+1, maxDiffLines+1))
	for line := range strings.SplitSeq(trimmed, "\n") {
		if len(lines) >= maxDiffLines {
			lines = append(lines, "... (これ以降は省略)")
			break
		}
		lines = append(lines, sanitizeDetailLine(line))
	}
	if colored {
		lines = HighlightDiff(lines)
	}
	return lines
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
// ⚠️ `:(top,literal)` を必ず付ける。両方に理由がある:
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

// worktreePathspec は root 相対パスを cwd 非依存・glob 無効の pathspec にする (理由は pathspecs)。
func worktreePathspec(path string) string { return ":(top,literal)" + path }
