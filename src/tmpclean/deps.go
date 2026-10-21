// Package tmpclean は一時領域 (Claude Code の scratchpad と $TMPDIR の直下) を数え、ユーザーが承認した古いものだけを
// ゴミ箱へ移す CLI の本体。公開するのは Run だけで、cmd/tmpclean の main はそれを呼ぶだけ。
//
// 判定 (生きている claude・使用中のパス・古さ) は外から推測するしかなく、完全には見分けられない
// (README の「検出しないもの」)。守りは層を重ね、最後の層をゴミ箱 (戻せる) にしている。
package tmpclean

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"safefs"
	"tmpclean/internal/runner"
)

// deps は外へ出る口と環境の値。production は prodDeps、テストは使い捨ての dir と fake の runner を入れる。
type deps struct {
	run         runner.Runner
	sessionRoot string // Claude Code の scratchpad の根 (/private/tmp/claude-<uid>)
	tmpDirEnv   string // $TMPDIR の生の値 (空 = 無い)
	trashRoot   string // ゴミ箱 (~/.Trash)
	selfSID     string // 自分の CLAUDE_CODE_SESSION_ID
	selfPID     int    // 祖先をたどって自分の claude を探す起点
	now         func() time.Time
	// inner は safefs に渡す検査点の内側。production は safefs.ProductionGuard (テストのバイナリの中では拒否する)、
	// テストは safefs/sandbox。tmpclean 自身の検査 (根の中・深さちょうど) はこの外側に必ず被せる (trashGuard)
	inner safefs.Guard
	// strictRoots は根が本物の形か (/private/tmp/claude-<uid>・/private/var/folders/*/*/T) を検査するか。
	// テストだけが false にする (根を os.TempDir() 配下の使い捨て dir に向けるため)。CLI には差し替えの口を出さない
	strictRoots bool
}

func prodDeps() deps {
	home, _ := os.UserHomeDir()
	trash := ""
	if home != "" {
		trash = filepath.Join(home, ".Trash")
	}
	return deps{
		run:         runner.Exec,
		sessionRoot: fmt.Sprintf("/private/tmp/claude-%d", os.Getuid()),
		tmpDirEnv:   os.Getenv("TMPDIR"),
		trashRoot:   trash,
		selfSID:     os.Getenv("CLAUDE_CODE_SESSION_ID"),
		selfPID:     os.Getpid(),
		now:         time.Now,
		inner:       safefs.ProductionGuard,
		strictRoots: true,
	}
}
