package main

// 本文中のファイルパスのジャンプモード (本文 pager の Tab)。仕様は docs/issues-viewer-spec.md の
// 「本文中のファイルパスは Tab のジャンプモードから開く」。パスの解決 (何をリンクにするか) は
// issues/filelink.go、画面上の位置は github.com/jiikko/dotfiles/src/tuikit/markdown の RenderLinks が持つ。ここは状態とキーだけ。

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/markdown"
	"glogx/issues"
)

// linkJump はジャンプモードの状態。zero value = モード外。
//
// 🚨 選択を FileLinks の添字だけで持たない。添字は「その幅・その本文で整形した結果」の中でだけ
// 意味を持ち、resize・見張りによる本文の読み直し・エディタから戻ったときの読み直しで別のリンクを
// 指しうる。(種類, dest, 同じ組の中の何番目か) を鍵にして毎回引き直す (reanchor)。
type linkJump struct {
	active bool
	kind   markdown.LinkKind
	dest   string
	nth    int
}

// bodyTextWidth は本文を整形する幅 (引き出しの内側から、スクロールバーと行番号の溝を引く)。
// 🚨 描画 (bodyLines) とキー処理 (リンク一覧を引く) の両方がこれを使う。式を 2 箇所に書くと、
// 片方だけ幅が違ってキー側のリンク一覧と画面の強調が食い違う。
func (v *issuesView) bodyTextWidth(inner int) int {
	return inner - layout.ScrollbarWidth - srcGutterWidth(v.body.SrcLineCount())
}

// jumpLinks は今の幅で開けるリンクの一覧 (vp はキー処理の窓。描画側は bodyLines が同じ幅で引く)。
func (v *issuesView) jumpLinks(vp issuesViewport) []issues.FileLink {
	return v.body.FileLinks(v.bodyTextWidth(v.bodyWidth(vp.width)), v.jumpRepos())
}

// jumpRepos は「repo の中」とみなす範囲 (同じ repo の全 checkout。issues.LinkBase.Repos の doc)。
// git を 1 回叩くので root ごとに覚える (描画のたびに叩かない)。
func (v *issuesView) jumpRepos() []string {
	if v.linkReposRoot != v.root || v.linkRepos == nil {
		v.linkRepos, v.linkReposRoot = nil, v.root
		if v.root != "" {
			v.linkRepos = issues.WorktreeRoots(v.root)
		}
	}
	return v.linkRepos
}

// reanchor は鍵に一致するリンクの添字を返す。一致が無ければ -1。
func (j *linkJump) reanchor(fl []issues.FileLink) int {
	n := 0
	last := -1
	for i, l := range fl {
		if l.Kind != j.kind || l.Dest != j.dest {
			continue
		}
		if n == j.nth {
			return i
		}
		n++
		last = i
	}
	return last // 同じ dest の出現が減った (本文が書き換わった) ときは、残った最後の出現へ
}

// selectLink は fl[i] を選択にして鍵を覚え、選択の行が窓に入るよう本文をスクロールする。
func (v *issuesView) selectLink(fl []issues.FileLink, i, rows int) {
	l := fl[i]
	nth := 0
	for _, o := range fl[:i] {
		if o.Kind == l.Kind && o.Dest == l.Dest {
			nth++
		}
	}
	v.linkJump = linkJump{active: true, kind: l.Kind, dest: l.Dest, nth: nth}
	p := &v.bodyPager
	if l.Top < p.Offset || l.Top >= p.Offset+rows {
		p.Stop()
		p.Offset = max(l.Top-rows/3, 0) // 窓の上 1/3 に置く (端に置くと前後の文脈が見えない)
	}
}

// startLinkJump はジャンプモードに入る。今見えている窓の最初のリンクを選ぶ (無ければ窓より後ろの
// 最初、それも無ければ先頭)。開けるリンクが無ければ入らずに通知する。
func (v *issuesView) startLinkJump(vp issuesViewport, rows int) {
	fl := v.jumpLinks(vp)
	if len(fl) == 0 {
		v.setNotice("この issue に開けるファイルパスはありません", noticeRefused)
		return
	}
	top := v.bodyPager.Offset
	pick := 0
	for i, l := range fl {
		if l.Top >= top {
			pick = i
			break
		}
	}
	v.selectLink(fl, pick, rows)
}

// linkJumpKey はジャンプモード中のキーを捌く。handled=false なら、モードを抜けたうえで
// 呼び出し側が通常の本文 pager のキーとして処理する (死んだキーを作らない)。
func (v *issuesView) linkJumpKey(key string, vp issuesViewport, rows int) (cmd tea.Cmd, handled bool) {
	fl := v.jumpLinks(vp)
	cur := v.linkJump.reanchor(fl)
	if cur < 0 {
		// 本文が読み直されて選択していたリンクが消えた。抜けて、キーは通常どおり処理する
		v.linkJump = linkJump{}
		return nil, false
	}
	switch key {
	case "j", "down", "ctrl+n", "tab":
		v.selectLink(fl, (cur+1)%len(fl), rows)
	case "k", "up", "ctrl+p", "shift+tab":
		v.selectLink(fl, (cur-1+len(fl))%len(fl), rows)
	case "g", "home":
		v.selectLink(fl, 0, rows)
	case "G", "end":
		v.selectLink(fl, len(fl)-1, rows)
	case "enter":
		return v.openLink(fl[cur], true), true
	case "e", "v":
		// e は「エディタで開く」の語彙 (docs/glogx-ui-guide.md §2)。ジャンプ中は選択中のファイルへ効かせる
		// (抜けて issue 自体を開くと、光っているものと開くものが食い違う)
		return v.openLink(fl[cur], false), true
	case "y":
		v.copyText(fl[cur].Path, "パスをコピーしました: ")
	case "esc", "q", "h", "left":
		v.linkJump = linkJump{} // 1 段戻る (本文は閉じない)
	default:
		v.linkJump = linkJump{}
		return nil, false
	}
	return nil, true
}

// openLink は選択中のファイルを開く。readonly=true は Enter、false は $EDITOR (e)。
// Enter で開く先は種類で分ける (ユーザー選定 2026-09-28): **.md は viewer の本文 pager に積んで開く**
// (整形して読め、h/Esc で元の本文へ戻る。TUI を中断して別アプリへ移らない)。それ以外のファイルと
// ディレクトリは nvim -R (readonlyCommand。コードはシンタックスと検索、ディレクトリはファイラーが要る)。
//
// 🚨 開く直前に解決をやり直す (Body.Recheck): 一覧を作った後に消えた・ディレクトリに化けた・repo の外への
// symlink に差し替わった (git pull) ものを開かない。消えたものを開くと nvim は空の新規バッファを
// 「そのファイル」として見せる。
func (v *issuesView) openLink(l issues.FileLink, readonly bool) tea.Cmd {
	if !v.body.Recheck(l, v.jumpRepos()) {
		v.setNotice("ファイルが見つかりません: "+v.linkLabel(l), noticeRefused)
		return nil
	}
	if !readonly {
		return runEditorCmd(editorCommand(l.Path))
	}
	if !l.Dir && isMarkdownPath(l.Path) {
		v.openDoc(l)
		return nil
	}
	return runEditorCmd(readonlyCommand(l.Path, l.Line))
}

// maxDocDepth は docStack に積める段数 (相互リンクした doc を往復して積み続けないため)。
const maxDocDepth = 8

// bodyFrame は docStack に積んだ 1 段 (戻ったときに元の位置・ジャンプの選択まで戻す)。
type bodyFrame struct {
	open   *issues.Issue
	body   *issues.Body
	offset int
	jump   linkJump
}

// isMarkdownPath は viewer の本文 pager で開くファイルか (整形器が markdown 専用なので拡張子で決める)。
func isMarkdownPath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

// openDoc は .md のリンク先を本文 pager に積んで開く (h/Esc = popDoc で戻る)。
//
// doc は issue ではないが、表示・コピー・編集・URL・さらに先へのジャンプを本文と同じ経路で通すため、
// Issue の形に包む。🚨 Dir は元の issue のものを引き継ぐ: インラインコードの基準 (プロジェクトの root) は
// issue ディレクトリの親から決まる (LinkBase.Project)。doc 自身のディレクトリにすると基準がずれる。
// 番号・状態は持たないので p は「番号が無いのでファイル名」に落ち、n (claim) は一覧でしか効かない。
func (v *issuesView) openDoc(l issues.FileLink) {
	if len(v.docStack) >= maxDocDepth {
		// 相互リンクした doc を Enter で往復すると際限なく積まれ、抜けるのに h を何回も押すことになる
		v.setNotice("これ以上は積めません ("+strconv.Itoa(maxDocDepth)+" 段)。h で戻ってから開いてください", noticeRefused)
		return
	}
	doc := &issues.Issue{Path: l.Path, Dir: v.open.Dir, Rel: v.pathLabel(l.Path)}
	body, err := doc.ReadBody()
	if err != nil {
		v.setNotice("開けませんでした: "+firstLine(err.Error()), noticeError)
		return
	}
	v.docStack = append(v.docStack, bodyFrame{open: v.open, body: v.body, offset: v.bodyPager.Offset, jump: v.linkJump})
	v.open, v.body = doc, body
	v.bodyPager.Reset()
	v.urlPick.close()
	v.linkJump = linkJump{}
	v.docLine = l.Line
}

// popDoc は積んだ doc を 1 段戻す。戻る段が無ければ false (呼び出し側が本文を閉じる)。
func (v *issuesView) popDoc() bool {
	n := len(v.docStack)
	if n == 0 {
		return false
	}
	f := v.docStack[n-1]
	v.docStack = v.docStack[:n-1]
	v.open, v.body = f.open, f.body
	v.bodyPager.Reset()
	v.bodyPager.Offset = f.offset
	v.linkJump = f.jump
	v.docLine = 0
	return true
}

// rootOpen は一覧と照合できる本文 = docStack の底の issue (doc を開いていなければ v.open)。
func (v *issuesView) rootOpen() *issues.Issue {
	if len(v.docStack) > 0 {
		return v.docStack[0].open
	}
	return v.open
}

// scrollToSrcLine はソース行 line を含む表示行が窓の上 1/3 に来るよう本文を送る (`x.md#L12` で開いたとき)。
func (v *issuesView) scrollToSrcLine(line, rows int) {
	nums := v.body.SrcLines()
	top := -1
	for i, n := range nums {
		if n > 0 && n <= line {
			top = i // ブロックの先頭の表示行にだけ番号がある。line 以下で最後のものがその行を含む
		}
		if n > line {
			break
		}
	}
	if top >= 0 {
		v.bodyPager.Stop()
		v.bodyPager.Offset = max(top-rows/3, 0)
	}
}

// linkLabel はリンクの表示名 (repo 相対。repo の外は ~ 始まりか絶対パス)。本文に書かれた値ではなく
// **解決した先**を出す: 共通名 (README.md / Makefile) は別の repo のつもりで書かれていても
// ここの同名ファイルに当たるので、どこを開くのかを画面で確かめられるようにする。
func (v *issuesView) linkLabel(l issues.FileLink) string {
	p := v.pathLabel(l.Path)
	if l.Dir {
		p += "/"
	}
	if l.Line > 0 {
		p += ":" + strconv.Itoa(l.Line)
	}
	return p
}

// pathLabel は p の表示名 (repo 相対。repo の外は ~ 始まりか絶対パス。無害化済み)。
func (v *issuesView) pathLabel(p string) string {
	if rel, err := filepath.Rel(v.root, p); err == nil && v.root != "" && !strings.HasPrefix(rel, "..") {
		p = rel
	} else if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		p = "~" + p[len(home):]
	}
	return sanitizePlainLine(p)
}

// linkJumpStatus はジャンプ中にヘッダー 2 行目 (状態の行) へ出す文言 (行数を変えないため差し替える)。
func (v *issuesView) linkJumpStatus(fl []issues.FileLink, cur int) string {
	return "パス " + strconv.Itoa(cur+1) + "/" + strconv.Itoa(len(fl)) + ": " + v.linkLabel(fl[cur])
}
