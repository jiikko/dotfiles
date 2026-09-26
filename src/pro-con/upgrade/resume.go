package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// 入れ替えの前後で引き継ぐ状態。新しいプロセスへは環境変数 ResumeEnv でファイルのパスを渡す。

const (
	ResumeEnv = "PRO_CON_RESUME"
	Version   = 1
	// pendingEnv は shim が --async のとき「裏でビルドを spawn した」印として立てる env。pro-con の shim の呼び方は
	// --async を使わないので自分では立たないが、親 (glogx の popup 等) から継承した値を新しいプロセスへ渡さない
	pendingEnv = "GO_AUTOBUILD_PENDING"
)

var ErrVersion = errors.New("引き継ぐ状態の版が違う")

// State は引き継ぐ状態。UI と Backend の中身はそれぞれの持ち主が決める (ここは運ぶだけ)。
type State struct {
	Version int             `json:"version"`
	UI      json.RawMessage `json:"ui,omitempty"`
	Backend json.RawMessage `json:"backend,omitempty"`
}

// Save は状態を書く (一時ファイル → rename。途中で落ちても壊れたファイルを残さない)。path が空なら dir に新しく作り、
// 空でなければそこへ上書きする (引き継いだファイルを次の切り替えでも使い回す = 溜めない・消したパスを案内しない)。
func Save(dir, path string, st State) (string, error) {
	st.Version = Version
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	if path == "" {
		path = filepath.Join(dir, fmt.Sprintf("resume-%d-%d.json", os.Getpid(), time.Now().UnixNano()))
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// Load は状態を読む。消すのは呼び出し側 (正常に終わったとき / 次へ引き継いだとき。異常終了では残して案内する)。
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("引き継ぐ状態を読めない: %w", err)
	}
	if st.Version != Version {
		return State{}, fmt.Errorf("%w (ファイル %d / この版 %d)", ErrVersion, st.Version, Version)
	}
	return st, nil
}

// ExecFunc は syscall.Exec と同じ形 (テストで差し替える)。成功すると戻らない。
type ExecFunc func(argv0 string, argv []string, envv []string) error

// Exec は exe を新しいプロセスとして起動し直す (PID と端末はそのまま)。状態のファイルを環境変数で渡し、
// shim の印 (GO_AUTOBUILD_PENDING) は落とす。戻ってきたら失敗 (呼び出し側は旧版のまま続ける)。
func Exec(exe string, args []string, env []string, statePath string, execFn ExecFunc) error {
	return execFn(exe, append([]string{exe}, args...), Env(env, ResumeEnv+"="+statePath))
}

// Env は入れ替えの先へ渡す環境: env から前の入れ替えの印 (ResumeEnv) と shim の印 (GO_AUTOBUILD_PENDING)、
// set で渡す名前の古い値を落とし、set ("NAME=値") を足す。
func Env(env []string, set ...string) []string {
	drop := []string{ResumeEnv + "=", pendingEnv + "="}
	for _, kv := range set {
		if i := strings.IndexByte(kv, '='); i > 0 {
			drop = append(drop, kv[:i+1])
		}
	}
	out := make([]string, 0, len(env)+len(set))
	for _, kv := range env {
		if !slices.ContainsFunc(drop, func(p string) bool { return strings.HasPrefix(kv, p) }) {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}
