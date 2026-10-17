package issuesview

import (
	"time"

	"glogx/internal/app"
	"glogx/issues"
)

// issues viewer を開いたまま終了したときの「最後に見ていた画面」の保存 (ユーザー要望 2026-07-31)。
//
// glogx は tmux popup から C-g で開閉するトグルとして使われる。viewer を開いたまま C-g で
// 閉じると、次に開いたとき git log の一覧から辿り直しになるため、画面を 1 つだけ覚えておく。
//
// 🚨 覚えるのは「viewer を出したまま終了したとき」だけ。git log 一覧から終了したときは消す
// (ユーザー指定)。消さないと 2 回前に見ていた viewer が後から蘇り、「一覧を見て閉じたのに次は
// viewer が出る」という予測できない挙動になる。
//
// ファイルは 1 本だけ持ち、repo は中身の Root で照合する。repo ごとに持たないのは、起動時に
// 「保存があるか」を知るのに repo root の解決 (git fork) を先に走らせたくないため: 保存が無い
// 通常の起動 (大半) で fork が 1 本増える。1 本にしておけば、ファイルを読んで TTL 切れなら
// そこで終われる。代償は「repo A → repo B と開くと A の記憶が消える」だが、TTL が短いので
// 「直前に見ていた画面」の意味は変わらない。

// issuesScreen は復元に必要な最小限の画面状態。
//
// issue は番号でも basename でもなくパスで指す (仕様が定める同一性キー。
// docs/issues-viewer-spec.md 2 節)。復元時に見つからなければ黙って別の issue を出さず、
// 当たらなかったぶんだけ既定へ落ちる。
type IssuesScreen struct {
	Root    string    `json:"root"`     // スキャンの起点 (別 repo の画面を復元しないための照合キー)
	SavedAt time.Time `json:"saved_at"` // TTL 判定
	Tab     string    `json:"tab"`      // カテゴリタブ名 ("" = All)
	// Filter は表示段階の名前 ("open" / "pending" / "all")。🚨 序数で持たないこと: 段階を増減・
	// 並べ替えると保存済みの値が別の段階を指す (issues.StatusFilter.String の注記)。
	Filter  string          `json:"filter"`
	Cursor  string          `json:"cursor"`           // 一覧のカーソル行の issue パス
	Open    string          `json:"open"`             // 本文を開いていた issue のパス ("" = 一覧のみ)
	BodyOff int             `json:"body_off"`         // 本文のスクロール位置
	Groups  map[string]bool `json:"groups,omitempty"` // 展開済みの Epic GroupKey だけ
	// CursorGroup はカーソルが Epic の親行にあったときの GroupKey ("" = issue 行)。親行は
	// issue の path を持たないので Cursor では表せない (再スキャン側の currentGroupKey と対)。
	CursorGroup string `json:"cursor_group,omitempty"`
}

// issuesMemory は issues viewer の記憶 ($XDG_CACHE_HOME/glog/issues-last-screen.json)。作法は screen_memory.go。
var issuesMemory = app.ScreenMemory[IssuesScreen]{File: "issues-last-screen.json", SavedAt: func(s IssuesScreen) time.Time { return s.SavedAt }}

func SaveIssuesScreen(s IssuesScreen) error { return issuesMemory.Save(s) }

// removeIssuesScreen は記憶を捨てる (git log 一覧から終了したとき)。
func RemoveIssuesScreen() { issuesMemory.Remove() }

// loadIssuesScreen は TTL 内の記憶を返す (読めない・壊れている・期限切れ・未来の時刻は「記憶なし」。app.ScreenMemory.load)。
//
// repo の照合はここではしない: 呼び出し側が Root と実際の repo root を突き合わせる
// (root の解決は git fork なので、記憶があるときだけ非同期で行う)。
func LoadIssuesScreen(now time.Time) (IssuesScreen, bool) {
	s, ok := issuesMemory.Load(now)
	if !ok || s.Root == "" {
		return IssuesScreen{}, false
	}
	// 外部ファイル由来の値はここで正規化する。知らない段階名は「open のみ」に倒す
	// (見えすぎるより見えなさすぎる方が、a を押せば戻せるぶん安全)
	if f, ok := issues.ParseStatusFilter(s.Filter); !ok {
		s.Filter = f.String()
	}
	if s.BodyOff < 0 {
		s.BodyOff = 0
	}
	return s, true
}
