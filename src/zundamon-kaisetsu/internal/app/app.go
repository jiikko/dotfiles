// Package app は CLI の実行の文脈: 環境 (Env。テストで差し替える口)・利用者に見せる失敗 (Fail / ErrExit)・中断 (Ctx / InterruptedErr)。
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"zundamon-kaisetsu/internal/paths"
)

// CtxInterrupted は ctx が取り消された (中断された) なら ErrInterrupted を返す。🚨 全体の Ctx ではなく渡された ctx で見る:
// 見張りは Ctx と別の ctx (CleanupContext) で StopContainers を呼ぶので、Ctx で判定すると、見張りが SIGTERM を受けている
// 間の成功 (not found) や本物の失敗まで「中断した」になる (issue 652 の敵対的レビュー)。時間切れ (DeadlineExceeded) は中断ではない
func CtxInterrupted(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return ErrInterrupted
	}
	return nil
}

const SkillDirEnv = "ZUNDAMON_KAISETSU_SKILL_DIR"

// Env は外の世界 (出力先・エンジン・skill の置き場・時計) をまとめたもの。テストで差し替える。
type Env struct {
	Engine   string
	SkillDir string
	// AssetsFaces は立ち絵の既定の置き場 (skill のディレクトリからの相対)。素材の再配布になるので公開リポジトリには置かない。
	// dotfiles では非公開リポジトリをここにサブモジュールとして置いている。それ以外の環境は README の Setup で用意する
	AssetsFaces    string
	Stdout, Stderr io.Writer
	Now            func() time.Time
	// EngineUp / EngineDown / SpawnReaper はエンジンの自動起動と、使われなくなったら止める見張り (engine_auto.go)。
	// nil なら自動で管理しない (テストは本物のコンテナに触れない)。StateDir は印・最後に使った時刻・見張りのロックの置き場
	EngineUp    func() error
	EngineDown  func(runtime, engine string) error
	SpawnReaper func() error
	StateDir    string
	Sleep       func(time.Duration)
	LoadAvg     func() (float64, bool)
	// MeasureLabels はラベルの改行の計測 (build.CheckLabelLayout。issue 812) を差し替える口。計測モードの HTML のパスを受け取り、
	// player.html の #measure が書く JSON ({"labels": [...]}) を返す。nil なら本物の Chrome で測る (テストは本物の Chrome を起こさない)
	MeasureLabels func(page string) ([]byte, error)
}

// Template はプレイヤーのテンプレート。
func (e *Env) Template() string { return filepath.Join(e.SkillDir, "templates", "player.html") }

func (e *Env) SetSkillDir(d string) {
	e.SkillDir = d
	e.AssetsFaces = ""
	if d != "" {
		e.AssetsFaces = filepath.Join(d, "assets", "zundamon-kaisetsu", "faces")
	}
}

// RequireSkillDir は skill のディレクトリが要るコマンドで、それが渡されていなければ止める。
// 黙って続けると、立ち絵の既定の置き場を見ずに丸アバターの動画ができる (issue 641 の設計レビュー P2-10)。
func (e *Env) RequireSkillDir() error {
	if e.SkillDir == "" {
		return Fail("%s が無い。bin/zundamon-kaisetsu から起動する (skill のディレクトリをこの環境変数で渡す)", SkillDirEnv)
	}
	if !paths.IsFile(e.Template()) {
		return Fail("%s=%s に templates/player.html が無い (skill のディレクトリを指していない)", SkillDirEnv, e.SkillDir)
	}
	return nil
}

// --- 中断 (Ctrl-C) ---
//
// Ctx は SIGINT / SIGTERM / SIGHUP で取り消される。外部コマンド・HTTP・待ちはすべてこれに結び付ける。取り消されると
// Go が子プロセスを止め、Wait は子が終わってから戻るので、呼び出し側の defer (一時ディレクトリ・一時ファイルの削除) は
// 子が止まった後に、後から登録したものから順に走る。Python 版の KeyboardInterrupt と同じ順序 (敵対的レビュー 2 周目 P2-1。
// 自前の後始末の登録表は、子を止める前に一時ディレクトリを消しにいき、書き続ける子に消し残された)。
// 後始末の後は同じ signal を受け直して死ぬ (rc=130 で普通に終えると、bash のループが Ctrl-C で止まらない。同 P2-2)。
var Ctx = context.Background()

// SleepCtx は Ctx が取り消されたら待つのをやめる time.Sleep。
func SleepCtx(d time.Duration) {
	select {
	case <-Ctx.Done():
	case <-time.After(d):
	}
}

// ErrInterrupted は中断で処理を打ち切ったことを表す (main が同じ signal で死に直す)。
var ErrInterrupted = errors.New("中断した")

func InterruptedErr() error {
	if Ctx.Err() != nil {
		return ErrInterrupted
	}
	return nil
}

// AcronymsFile は 1 文字ずつ読むのが正しい略語 (CPU・OS など) の一覧。skill の dir に読み辞書と並べて置く。
func (e *Env) AcronymsFile() string { return filepath.Join(e.SkillDir, "acronyms.json") }

// ErrExit は利用者に見せる失敗 (main が "error: " を付けて rc=1 で終える)。
type ErrExit struct {
	Msg string
}

func (e *ErrExit) Error() string { return e.Msg }

func Fail(format string, a ...any) error { return &ErrExit{fmt.Sprintf(format, a...)} }
