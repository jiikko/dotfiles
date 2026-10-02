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

// Claude の利用枠の取得は、全プロセス (glogx の U / R・bin/ratelimit・複数の glogx) で 1 つの
// ゲートを通す (issue 627)。`claude -p /usage` は 1 回ごとにサーバの `/api/oauth/usage` を叩き、
// これは bearer ごとに強く rate limit される: glogx が表示中に 60 秒ごとに取ったところ 17 回目で
// 429 になり、以後 CLI は約 45 分 (Retry-After) 枠の行を黙って省いた (実測 2026-10-02、2.1.287)。
// 呼び出し側ごとに周期を下げても、取得元が増えれば合計が増えるので、上限は取得の手前の 1 か所で持つ。
const (
	sharedFile = "claude-usage-shared.json"
	sharedLock = "claude-usage-shared.lock"
	// sharedFresh: 誰かが取ってからこの時間は、claude を起こさずその結果を返す。429 になる前の
	// 実績 (hook のキャッシュ 5 分 + glogx の不定期の取得で、1 時間に 2〜8 回) に合わせた。
	sharedFresh = 5 * time.Minute
	// noLimitsBackoff: サーバが枠を返さなかった後、claude を起こさない時間。CLI 自身も 429 の
	// Retry-After (実測 2692 秒) を覚えていてその間は問い合わせないが、こちらが毎回起こすと
	// 1 回 ~2 秒の node が無駄に回るうえ、覚えが切れた瞬間にまた叩きに行く。
	noLimitsBackoff = 30 * time.Minute
)

// errNoLimits は CLI がサーバから利用枠を受け取れなかったこと (usage_report.rate_limits が null)。
var errNoLimits = errors.New("サーバが利用枠を返さない (/api/oauth/usage の 429 と思われる)")

// clock は現在時刻 (テストで差し替える)。
var clock = time.Now

// sharedState は共有ファイルの中身。Snapshot は最後に取れた Claude の枠 (codex 枠は入れない)。
type sharedState struct {
	Snapshot     *Snapshot `json:"snapshot,omitempty"`
	FetchedAt    time.Time `json:"fetchedAt,omitzero"`
	BlockedUntil time.Time `json:"blockedUntil,omitzero"`
}

// Fetch は Claude の利用枠を返す。sharedFresh 以内に誰かが取った結果があればそれを返し、
// サーバが枠を返さず止めている間は claude を起こさずエラーを返す。それ以外は `claude -p /usage`
// を起こして結果を共有ファイルへ書く。同時に呼ばれても claude を起こすのは 1 本だけ (flock)。
//
// キャッシュの置き場が決まらない (HOME も XDG_CACHE_HOME も無い) ときは、共有せず直接取る。
func Fetch(ctx context.Context) (*Snapshot, error) {
	dir, err := cachedir.Base()
	if err != nil {
		return fetchClaude(ctx)
	}
	return fetchShared(ctx, dir)
}

func fetchShared(ctx context.Context, dir string) (*Snapshot, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("利用枠の共有ファイルの置き場を作れない: %w", err)
	}
	unlock, err := lockShared(ctx, filepath.Join(dir, sharedLock))
	if err != nil {
		return nil, err
	}
	defer unlock()

	path := filepath.Join(dir, sharedFile)
	st := loadShared(path)
	now := clock()
	if st.Snapshot != nil {
		if age := now.Sub(st.FetchedAt); age >= 0 && age < sharedFresh {
			return st.Snapshot, nil
		}
	}
	if now.Before(st.BlockedUntil) {
		return nil, blockedErr(st.BlockedUntil)
	}
	snap, err := fetchClaude(ctx)
	switch {
	case err == nil:
		st.Snapshot, st.FetchedAt, st.BlockedUntil = snap, now, time.Time{}
	case errors.Is(err, errNoLimits):
		// 最後に取れた枠は残す (呼び出し側の last-good とは別に、次の成功までの手掛かり)
		st.BlockedUntil = now.Add(noLimitsBackoff)
		err = blockedErr(st.BlockedUntil)
	default:
		return nil, err
	}
	// 書けなくても今回の結果は正しい。次の呼び出しがもう一度取るだけなので、失敗は返さない
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
			return nil, fmt.Errorf("利用枠の共有ロック待ちで打ち切り: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// loadShared は共有ファイルを読む。無い・壊れているときは空 (= 取りに行く)。表示に載る文字列は
// ここで termsafe を通す (別プロセスが書いたファイルなので、端末へ出す前に制御列を落とす)。
func loadShared(path string) sharedState {
	data, err := os.ReadFile(path)
	if err != nil {
		return sharedState{}
	}
	var st sharedState
	if err := json.Unmarshal(data, &st); err != nil {
		return sharedState{}
	}
	if st.Snapshot != nil {
		if len(st.Snapshot.Windows) == 0 {
			st.Snapshot = nil
		} else {
			for i := range st.Snapshot.Windows {
				st.Snapshot.Windows[i].Label = termsafe.PlainLine(st.Snapshot.Windows[i].Label)
			}
			st.Snapshot.Version = termsafe.PlainLine(st.Snapshot.Version)
			st.Snapshot.ClaudeErr = ""
		}
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
