package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"atomicfile"
	"doctor/cachedir"

	"github.com/jiikko/dotfiles/src/termsafe"
)

// Claude の利用枠の取得は、`claude -p /usage` を起こす全ての呼び出し元 (glogx の U / R・bin/ratelimit・
// pro-con の dispatcher) で 1 つのゲートを通す (issue 627)。`claude -p /usage` は 1 回ごとにサーバの
// `/api/oauth/usage` を叩き、これは bearer ごとに強く rate limit される: glogx が表示中に 60 秒ごとに
// 取ったところ 17 回目で 429 になり、以後 CLI は約 45 分 (Retry-After) 枠の行を黙って省いた
// (実測 2026-10-02、2.1.287)。呼び出し元ごとに周期を下げても、呼び出し元が増えれば合計が増えるので、
// 上限は取得の手前の 1 か所で持つ。
const (
	sharedFile = "claude-usage-shared.json"
	sharedLock = "claude-usage-shared.lock"
	// SharedFresh: 誰かが claude を起こしてからこの時間は、成否によらず起こし直さない (成功ならその
	// 結果を、失敗ならその理由を返す)。429 になる前の実績 (1 時間に 2〜8 回) に合わせた。
	SharedFresh = 5 * time.Minute
	// noLimitsBackoff: サーバから枠を受け取れなかった後、claude を起こさない時間。CLI 自身が 429 の
	// Retry-After (実測 2692 秒) を覚えていて、その間は起こしてもサーバへは行かない。ここで止めるのは
	// node を無駄に起こさないためなので短めでよく、通信断で null になった場合の取りこぼしも小さくする。
	noLimitsBackoff = 10 * time.Minute
)

// errNoLimits は CLI がサーバから利用枠を受け取れなかったこと (usage_report.rate_limits が null)。
// 429 のほか通信の失敗でも同じ形になりうる (区別する手段が CLI の出力に無い)。
var errNoLimits = errors.New("サーバから利用枠を受け取れない (429 か通信の失敗)")

// clock は現在時刻 (テストで差し替える)。
var clock = time.Now

// sharedState は共有ファイルの中身。Snapshot は最後に取れた Claude の枠 (codex 枠は入れない)。
type sharedState struct {
	Snapshot  *Snapshot `json:"snapshot,omitempty"`
	FetchedAt time.Time `json:"fetchedAt,omitzero"`
	// AttemptedAt / LastErr は最後に claude を起こして失敗した時刻と理由 (成功したら消す)。
	// 読めない応答 (書式変更等) のたびに起こし直すと、それ自体がサーバを高頻度に叩く。
	AttemptedAt  time.Time `json:"attemptedAt,omitzero"`
	LastErr      string    `json:"lastErr,omitempty"`
	BlockedUntil time.Time `json:"blockedUntil,omitzero"`
}

// Fetch は Claude の利用枠を、全プロセス共有のゲートを通して返す (FetchShared に fetchClaude を渡す)。
func Fetch(ctx context.Context) (*Snapshot, error) { return FetchShared(ctx, fetchClaude, false) }

// FetchShared は run (`claude -p /usage` を起こして読む関数) をゲート越しに呼ぶ。
//   - SharedFresh 以内に誰かが取れていれば、run を呼ばずその結果を返す
//   - SharedFresh 以内に誰かが失敗していれば、run を呼ばずその理由を返す
//   - サーバから枠を受け取れず止めている間は、run を呼ばずエラーを返す
//
// force は前の 2 つを飛ばす (人が「今すぐ取り直す」を押したとき。止めている間は force でも呼ばない)。
// 同時に呼ばれても run を呼ぶのは 1 本だけ (flock)。キャッシュの置き場が決まらない (HOME も
// XDG_CACHE_HOME も無い) ときは、共有せず直接呼ぶ。
func FetchShared(ctx context.Context, run func(context.Context) (*Snapshot, error), force bool) (*Snapshot, error) {
	dir, err := cachedir.Base()
	if err != nil {
		return run(ctx)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("利用枠の共有ファイルの置き場を作れない: %w", err)
	}
	unlock, err := lockShared(ctx, filepath.Join(dir, sharedLock))
	if err != nil {
		return nil, err
	}
	defer unlock()

	path := filepath.Join(dir, sharedFile)
	now := clock()
	st := loadShared(path, now)
	if !force && st.Snapshot != nil && now.Sub(st.FetchedAt) < SharedFresh {
		return st.Snapshot, nil
	}
	if now.Before(st.BlockedUntil) {
		return nil, blockedErr(st.BlockedUntil)
	}
	if !force && st.LastErr != "" && now.Sub(st.AttemptedAt) < SharedFresh {
		return nil, fmt.Errorf("%s (%s に取り直す)", st.LastErr, st.AttemptedAt.Add(SharedFresh).Local().Format("15:04"))
	}
	snap, err := run(ctx)
	switch {
	case err == nil:
		st.Snapshot, st.FetchedAt = snap, now
		st.AttemptedAt, st.LastErr, st.BlockedUntil = time.Time{}, "", time.Time{}
	case errors.Is(err, errNoLimits):
		// 最後に取れた枠は残す (呼び出し側の last-good とは別に、次の成功までの手掛かり)
		st.BlockedUntil = now.Add(noLimitsBackoff)
		err = blockedErr(st.BlockedUntil)
	case ctx.Err() != nil:
		// 呼び出し側の持ち時間切れ・取り消しは、他の呼び出し元を止める理由にしない
		return nil, err
	default:
		st.AttemptedAt, st.LastErr = now, termsafe.PlainLine(err.Error())
	}
	// 書けなくても今回の結果は正しい。次の呼び出しがもう一度起こすだけなので、失敗は返さない
	_ = saveShared(path, st)
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func blockedErr(until time.Time) error {
	return fmt.Errorf("%w。%s まで取得を止める", errNoLimits, until.Local().Format("15:04"))
}

// lockShared は共有ファイルの排他を取る。ctx が終わるまで待ち、取れなければ ctx の理由を返す。
func lockShared(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("利用枠の共有ロックを開けない: %w", err)
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil // close で flock も外れる
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("利用枠の共有ロックを取れない: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("他のプロセスが利用枠を取得中で、待ちきれなかった: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// loadShared は共有ファイルを読む。無い・壊れているときは空 (= 取りに行く)。
//   - 時刻は now より未来のものを信用しない (時計の巻き戻り・壊れたファイルで、古い枠を「新しい」と
//     出し続けたり、何時間も止まり続けたりしないため)。止める期限は now + noLimitsBackoff まで
//   - 表示に載る文字列は termsafe を通す (別プロセスが書いたファイルなので、端末へ出す前に制御列を落とす)
func loadShared(path string, now time.Time) sharedState {
	data, err := os.ReadFile(path)
	if err != nil {
		return sharedState{}
	}
	var st sharedState
	if err := json.Unmarshal(data, &st); err != nil {
		return sharedState{}
	}
	if st.Snapshot != nil && (len(st.Snapshot.Windows) == 0 || st.FetchedAt.After(now)) {
		st.Snapshot, st.FetchedAt = nil, time.Time{}
	}
	if st.Snapshot != nil {
		for i := range st.Snapshot.Windows {
			st.Snapshot.Windows[i].Label = termsafe.PlainLine(st.Snapshot.Windows[i].Label)
		}
		st.Snapshot.Version = termsafe.PlainLine(st.Snapshot.Version)
		st.Snapshot.ClaudeErr = ""
	}
	if st.AttemptedAt.After(now) {
		st.AttemptedAt, st.LastErr = time.Time{}, ""
	}
	st.LastErr = termsafe.PlainLine(st.LastErr)
	if st.BlockedUntil.After(now.Add(noLimitsBackoff)) {
		st.BlockedUntil = time.Time{}
	}
	return st
}

func saveShared(path string, st sharedState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}
