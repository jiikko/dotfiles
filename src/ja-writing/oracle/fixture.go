package oracle

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"ja-writing/internal/pyfs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

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

// Namespace は argparse.Namespace (属性の名前 → 値)。
type Namespace map[string]any

// MarshalJSON は {"$namespace": {...}} にする。
func (n Namespace) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]map[string]any{"$namespace": n})
}

// pyString は s (本家の str と同じ字の WTF-8) を JSON の文字列にする。単独のサロゲートは "\udcff" の形の escape にする
// (encoding/json は U+FFFD に変える)。driver の json.loads はそれを単独のサロゲートを持つ str に読む。上位と下位のサロゲートが並ぶと
// json.loads が 1 字に合わせて別の str になるので、誤りにする。WTF-8 としても読めないバイトも誤りにする (本家の str に無い)。
func pyString(s string) ([]byte, error) {
	if utf8.ValidString(s) {
		return json.Marshal(s)
	}
	out := []byte{'"'}
	high := false
	for i := 0; i < len(s); {
		r, n := pyfs.DecodeRune(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			return nil, fmt.Errorf("WTF-8 でないバイト %#x (%q)", s[i], s)
		case r >= 0xD800 && r <= 0xDFFF:
			if high && r >= 0xDC00 {
				return nil, fmt.Errorf("上位と下位のサロゲートが並んでいる (%q)", s)
			}
			out = fmt.Appendf(out, "\\u%04x", r)
			high = r < 0xDC00
		default:
			quoted, err := json.Marshal(s[i : i+n])
			if err != nil {
				return nil, err
			}
			out = append(out, quoted[1:len(quoted)-1]...)
			high = false
		}
		i += n
	}
	return append(out, '"'), nil
}

// Argv は本家の main に渡す argv (本家の sys.argv と同じ字。単独のサロゲートも持てる)。JSON では pyString の形にする。
type Argv []string

// MarshalJSON は各要素を pyString にした JSON の配列にする。
func (a Argv) MarshalJSON() ([]byte, error) {
	out := []byte{'['}
	for i, s := range a {
		if i > 0 {
			out = append(out, ',')
		}
		b, err := pyString(s)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return append(out, ']'), nil
}

// Op は作業フォルダへの操作。Kind は write・mkdir・symlink・hardlink・mkfifo・chdir・chmod・remove・setenv・unsetenv・restoreenv・git・stdin・utime。
// utime は Path の mtime と atime を、今の時刻に Value の秒数を足した時刻にする。
// stdin は driver の sys.stdin を BytesB64 (か Content) を読む入力にする (Name が "none" なら None)。Go のテストは Apply を通さず、自分の標準入力の代わりに持つ。
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

// MarshalJSON は Value を pyString にする (setenv で単独のサロゲートを持つ値を渡す)。
func (o Op) MarshalJSON() ([]byte, error) {
	type plain Op
	value, err := pyString(o.Value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		plain
		Value json.RawMessage `json:"value"`
	}{plain(o), value})
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

// WriteData は op write が書くバイト列。BytesB64 があればそれ (中の RootToken は作業フォルダの実パスのバイト列になる)、なければ Content の UTF-8。
// 不正な UTF-8 を含む中身は JSON の文字列を通すと U+FFFD になって Python に別のバイト列が渡るので、BytesB64 で渡す。
func (f *Fixture) WriteData(op Op) ([]byte, error) {
	if op.BytesB64 == "" {
		return []byte(f.Sub(op.Content)), nil
	}
	data, err := base64.StdEncoding.DecodeString(op.BytesB64)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(data, []byte(RootToken), []byte(f.Root)), nil
}

// Apply は op 1 つを実行する。
func (f *Fixture) Apply(ctx context.Context, op Op) error {
	path := f.Sub(op.Path)
	switch op.Kind {
	case "write":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		data, err := f.WriteData(op)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	case "mkdir":
		return os.MkdirAll(path, 0o755)
	case "symlink":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.Symlink(f.Sub(op.Target), path)
	case "hardlink":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.Link(f.Sub(op.Target), path)
	case "mkfifo":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return syscall.Mkfifo(path, 0o644)
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
	case "utime":
		seconds, err := strconv.ParseFloat(op.Value, 64)
		if err != nil {
			return fmt.Errorf("utime の秒数を読めません: %w", err)
		}
		stamp := time.Now().Add(time.Duration(seconds * float64(time.Second)))
		return os.Chtimes(path, stamp, stamp)
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
