package filer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"safefs"
)

// fileops.go は木の上のファイル操作: a (新規作成)・m (名前の変更)・d (ゴミ箱へ移す)。
// 上書きしない・同じフォルダの外へ出ない・ゴミ箱への移動の本体は safefs (doctor と共有)。ここは入力欄・確認の板・読み直しとカーソルだけ。
// 名前はディスク上の名前 (node.raw) と絶対パス (node.abs) で扱う (表示名は termsafe を通した別物)。root そのものは名前を変えず、捨てない。

// fsGuard は safefs の検査点。🚨 テストは main_test.go で sandbox へ差し替え、一時ディレクトリの外を実行前に拒否する
// (`~/.claude/rules/sandbox-real-destructive-test-apis.md`)。既定はテストのバイナリの中では拒否する (差し替え忘れを止める)。
var fsGuard safefs.Guard = safefs.ProductionGuard

// trashDir はゴミ箱の場所 (Finder と同じ ~/.Trash。別ボリュームの項目は safefs が断る)。テストは一時ディレクトリへ差し替える。
var trashDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".Trash")
}

// promptKind は足元の入力欄の用途 (`!` のコマンド・名前の入力・`:` のパスを同じ欄で受ける。入力を握る層を増やさない)。
type promptKind int

const (
	promptShell  promptKind = iota // `!`
	promptCreate                   // a: target (フォルダ) の中に作る名前
	promptRename                   // m: target の新しい名前
	promptGoto                     // `:`: カーソルを飛ばす先のパス (goto.go)
)

// safeDir はフォルダ n の実体のパス (root だけ symlink を辿り、途中の symlink は断る。safefs.ResolveUnder の doc)。
func (m *Model) safeDir(n *node) (string, error) { return safefs.ResolveUnder(m.root.abs, n.abs) }

// identityOf は n の実体。親は safeDir で解決する (途中の symlink を辿らない)。
func (m *Model) identityOf(n *node) (safefs.ID, error) {
	dir, err := m.safeDir(n.parent)
	if err != nil {
		return safefs.ID{}, err
	}
	return safefs.Identity(filepath.Join(dir, n.raw))
}

// pin は「確かめてから実行する」操作 (名前の変更・ゴミ箱) の前段: root は断り、今の実体を取る。失敗は what を頭に知らせて ok=false。
func (m *Model) pin(n *node, what, rootMsg string) (safefs.ID, bool) {
	if n == m.root {
		m.fail(rootMsg)
		return safefs.ID{}, false
	}
	id, err := m.identityOf(n)
	if err != nil {
		m.fail(what + ": " + opErr(err))
		return safefs.ID{}, false
	}
	return id, true
}

// startCreate は a: カーソルのフォルダ (ファイルならその親) に作る名前を聞く。
func (m *Model) startCreate() {
	dir := m.cur
	if !dir.dir {
		dir = dir.parent
	}
	m.openPrompt(promptCreate, dir, "")
}

// startRename は m: カーソルの項目の新しい名前を聞く (今の名前を入れて開く)。
func (m *Model) startRename() {
	n := m.cur
	id, ok := m.pin(n, "名前を変えられません", "root の名前は変えられません (- で 1 段上へ行ってから)")
	if !ok {
		return
	}
	m.openPrompt(promptRename, n, n.raw)
	m.prompt.id = id
}

func (m *Model) openPrompt(kind promptKind, target *node, text string) {
	m.prompt = promptState{active: true, kind: kind, target: target, history: m.prompt.history, hist: len(m.prompt.history)}
	m.prompt.line.Insert(text)
	m.prompt.prefill = m.prompt.line.String()
}

// fileOpEnter は名前の入力欄の Enter。
func (m *Model) fileOpEnter(text string) {
	p := m.prompt
	switch p.kind {
	case promptCreate:
		m.create(p.target, text)
	case promptRename:
		if text != p.prefill { // 開いたまま Enter (制御文字を落とした表示のまま) では変えない
			m.rename(p.target, text, p.id)
		}
	case promptShell, promptGoto:
	}
}

// create は dir の中に name を作る。末尾の / (1 つだけ) はフォルダの印。
func (m *Model) create(dir *node, text string) {
	if !m.inTree(dir) {
		m.fail("作れません: フォルダが木から無くなりました")
		return
	}
	name, isDir := strings.CutSuffix(text, "/")
	if err := safefs.ValidName(name); err != nil {
		m.fail("作れません: " + termsafeLine(err.Error()))
		return
	}
	real, err := m.safeDir(dir)
	if err == nil {
		err = safefs.Create(real, name, isDir, fsGuard)
	}
	if err != nil {
		m.fail("作れません: " + opErr(err))
		return
	}
	m.ensureLoaded(dir)
	m.reloadDir(dir)
	dir.expanded = true
	if n := m.findNode(filepath.Join(dir.abs, name)); n != nil {
		m.setCur(n)
	}
}

// rename は n の名前を text へ変える (同じフォルダの中だけ)。id は入力欄を開いたときの n の実体。
func (m *Model) rename(n *node, text string, id safefs.ID) {
	if text == n.raw {
		return
	}
	if !m.inTree(n) {
		m.fail("名前を変えられません: 項目が木から無くなりました")
		return
	}
	if err := safefs.ValidName(text); err != nil {
		m.fail("名前を変えられません: " + termsafeLine(err.Error()))
		return
	}
	parent := n.parent
	real, err := m.safeDir(parent)
	if err == nil {
		err = safefs.Rename(real, n.raw, text, id, fsGuard)
	}
	if err != nil {
		m.fail("名前を変えられません: " + opErr(err))
		return
	}
	m.reloadDir(parent)
	if k := m.findNode(filepath.Join(parent.abs, text)); k != nil {
		m.setCur(k)
	} else {
		m.setCur(parent)
	}
}

// startTrash は d: 確認の板を開く。
func (m *Model) startTrash() {
	n := m.cur
	id, ok := m.pin(n, "ゴミ箱へ移せません", "root はゴミ箱へ移せません (- で 1 段上へ行ってから)")
	if !ok {
		return
	}
	what := "ファイル"
	if n.dir {
		what = "フォルダ (中身ごと)"
	}
	// id は板を開いたとき (= 人が対象を決めたとき) の実体。y を押すまでに差し替わった項目は動かさない
	m.ask = &askBoard{title: "ゴミ箱へ移す", lead: what + "をゴミ箱へ移します:", name: n.name, verb: "ゴミ箱へ移す",
		yes: func(m *Model) { m.trash(n, id) }}
}

// trash は n をゴミ箱へ移し、カーソルを隣の項目 (下 → 上 → 親) へ移す。
func (m *Model) trash(n *node, id safefs.ID) {
	if !m.inTree(n) {
		m.fail("ゴミ箱へ移せません: 項目が木から無くなりました")
		return
	}
	parent := n.parent
	next := parent
	if m.isKidOf(parent, n) {
		s := m.kids(parent)
		switch {
		case n.vidx+1 < len(s):
			next = s[n.vidx+1]
		case n.vidx > 0:
			next = s[n.vidx-1]
		}
	}
	nextAbs := next.abs
	real, err := m.safeDir(parent)
	if err == nil {
		_, err = safefs.TrashMove(filepath.Join(real, n.raw), trashDir(), id, fsGuard)
	}
	if err != nil {
		m.fail("ゴミ箱へ移せません: " + opErr(err))
		return
	}
	m.reloadDir(parent)
	if k := m.findNode(nextAbs); k != nil {
		m.setCur(k)
	} else {
		m.setCur(parent)
	}
	m.notices = append(m.notices, Notice{Text: "ゴミ箱へ移しました: " + n.name, OK: true})
}

func opErr(err error) string {
	switch {
	case errors.Is(err, safefs.ErrExists):
		return "同じ名前の項目が既にあります"
	case errors.Is(err, safefs.ErrSwapped), errors.Is(err, safefs.ErrUnidentified), errors.Is(err, os.ErrNotExist):
		return "項目が変わりました (r で読み直してください)"
	}
	return termsafeLine(err.Error()) // エラーにパスが入る
}

// reloadDir はフォルダ n を読み直す (ライブ更新の 1 件と、ファイル操作の後)。
func (m *Model) reloadDir(n *node) {
	n.reload()
	m.walker.forget(n.abs, false)
	m.dropGoneRecs(n.abs) // 消えた・名前の変わったフォルダの走査の結果を手放す (issue 727)
	clear(m.linkCache)    // 増えた・消えたファイルでリンクの解決が変わる
	m.moving = true
}
