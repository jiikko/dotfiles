package oracle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"subproc"
)

// RootToken は本家に渡す文字列と、本家の結果の文字列の中で、作業フォルダを指す印 (driver が実際のパスと入れ替える)。
// Go のテストも同じ印で作業フォルダを書き、結果の中の作業フォルダを印に戻して比べる。
const RootToken = "<<ROOT>>"

// Ref は前の呼び出しの Save した値 (Python の値そのもの。JSON を経由しない)。
type Ref string

// MarshalJSON は {"$ref": 名前} にする。
func (r Ref) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]string{"$ref": string(r)}) }

// Bytes は Python の bytes。
type Bytes []byte

// MarshalJSON は {"$bytes_b64": base64} にする。
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"$bytes_b64": base64.StdEncoding.EncodeToString(b)})
}

// Cps は Python の str をコードポイントの列で持つ (単独のサロゲートも持てる)。JSON の文字列にサロゲートを書くと encoding/json が U+FFFD に変える。
type Cps []rune

// MarshalJSON は {"$str": [コードポイント...]} にする。
func (c Cps) MarshalJSON() ([]byte, error) { return json.Marshal(map[string][]rune{"$str": c}) }

// Op は作業フォルダへの操作。Kind は write・mkdir・symlink・chdir・chmod・remove・setenv・unsetenv・restoreenv・git。
// パスと値の中の RootToken は作業フォルダに置き換わる。git は Path を cwd にして Args を渡す (引数は subproc.GitArgs を通したもの)。
// restoreenv は setenv・unsetenv で変えた環境変数を戻し、cwd を作業フォルダに戻す。
type Op struct {
	Kind     string   `json:"op"`
	Path     string   `json:"path,omitempty"`
	Content  string   `json:"content,omitempty"`
	BytesB64 string   `json:"bytes_b64,omitempty"`
	Target   string   `json:"target,omitempty"`
	Mode     *uint32  `json:"mode,omitempty"`
	Name     string   `json:"name,omitempty"`
	Value    string   `json:"value"`
	Args     []string `json:"args,omitempty"`
}

func needsGit(calls []Call) bool {
	for _, c := range calls {
		if c.Op != nil && c.Op.Kind == "git" {
			return true
		}
	}
	return false
}

// GitEnv は op git の環境変数 (空から作る。利用者の git の設定・認証・pager を持ち込まない)。git が空なら PATH の dir を足さない。HOME は作業フォルダ。
func GitEnv(git string) map[string]string {
	path := "/usr/bin:/bin"
	if git != "" {
		path = filepath.Dir(git) + ":" + path
	}
	return map[string]string{
		"PATH": path, "HOME": RootToken, "LC_ALL": "C",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_TERMINAL_PROMPT": "0",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
	}
}

// Fixture は Go のテストが作業フォルダに driver と同じ操作をするための実行器。Root は実パス (driver の cwd も実パス)。
type Fixture struct {
	Root     string
	git      string
	savedEnv map[string]*string
}

// NewFixture は root (実パスにして渡す) を作業フォルダにする実行器。op git があるときだけ使う git の絶対パスを PATH から解決する。
func NewFixture(root string) (*Fixture, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git が見つかりません: %w", err)
	}
	if git, err = filepath.Abs(git); err != nil {
		return nil, err
	}
	return &Fixture{Root: root, git: git, savedEnv: map[string]*string{}}, nil
}

// Sub は文字列の中の RootToken を作業フォルダにする。
func (f *Fixture) Sub(s string) string { return strings.ReplaceAll(s, RootToken, f.Root) }

// Unroot は結果の文字列の中の作業フォルダを RootToken に戻す。
func (f *Fixture) Unroot(s string) string { return strings.ReplaceAll(s, f.Root, RootToken) }

func (f *Fixture) remember(name string) {
	if _, ok := f.savedEnv[name]; ok {
		return
	}
	if v, set := os.LookupEnv(name); set {
		f.savedEnv[name] = &v
	} else {
		f.savedEnv[name] = nil
	}
}

// Restore は setenv・unsetenv で変えた環境変数を戻し、cwd を作業フォルダに戻す (op restoreenv と同じ)。
func (f *Fixture) Restore() error {
	for name, old := range f.savedEnv {
		var err error
		if old == nil {
			err = os.Unsetenv(name)
		} else {
			err = os.Setenv(name, *old)
		}
		if err != nil {
			return err
		}
	}
	f.savedEnv = map[string]*string{}
	return os.Chdir(f.Root)
}

// Apply は op 1 つを実行する。
func (f *Fixture) Apply(ctx context.Context, op Op) error {
	path := f.Sub(op.Path)
	switch op.Kind {
	case "write":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		data := []byte(f.Sub(op.Content))
		if op.BytesB64 != "" {
			var err error
			if data, err = base64.StdEncoding.DecodeString(op.BytesB64); err != nil {
				return err
			}
		}
		return os.WriteFile(path, data, 0o644)
	case "mkdir":
		return os.MkdirAll(path, 0o755)
	case "symlink":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.Symlink(f.Sub(op.Target), path)
	case "chdir":
		return os.Chdir(path)
	case "chmod":
		if op.Mode == nil {
			return errors.New("chmod に mode がありません")
		}
		return os.Chmod(path, os.FileMode(*op.Mode))
	case "remove":
		return os.RemoveAll(path)
	case "setenv":
		f.remember(op.Name)
		return os.Setenv(op.Name, f.Sub(op.Value))
	case "unsetenv":
		f.remember(op.Name)
		return os.Unsetenv(op.Name)
	case "restoreenv":
		return f.Restore()
	case "git":
		args := make([]string, len(op.Args))
		for i, a := range op.Args {
			args[i] = f.Sub(a)
		}
		cmd := subproc.CommandContext(ctx, f.git, args...)
		cmd.Dir = path
		for k, v := range GitEnv(f.git) {
			cmd.Env = append(cmd.Env, k+"="+f.Sub(v))
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(op.Args, " "), err, out)
		}
		return nil
	}
	return fmt.Errorf("未知の op: %s", op.Kind)
}
