package oracle

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"subproc"
)

//go:embed py/*.py
var drivers embed.FS

// Runner は本家の道具を環境を固定して動かす。Python は NewRunner が絶対パスで解決した 1 つだけを使う。
type Runner struct {
	// Python は python3 の絶対パス。env を空から作るので PATH に頼らず、起動のたびに同じ実体を呼ぶ。
	Python string
	Tree   Tree
	// HashSeed は PYTHONHASHSEED の値。空なら "0"。
	HashSeed string
}

// WithHashSeed は PYTHONHASHSEED だけを変えた Runner。
func (r Runner) WithHashSeed(seed string) Runner {
	r.HashSeed = seed
	return r
}

// NewRunner は今の環境の python3 を絶対パスで解決して Runner を作る。
func NewRunner(tree Tree) (Runner, error) {
	p, err := exec.LookPath("python3")
	if err != nil {
		return Runner{}, fmt.Errorf("python3 が見つかりません: %w", err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return Runner{}, fmt.Errorf("python3 の絶対パスを決められません: %w", err)
	}
	return Runner{Python: abs, Tree: tree}, nil
}

// PyInfo は記録に入れる Python と unicodedata のバージョン。
type PyInfo struct {
	PythonVersion      string `json:"python_version"`
	UnicodedataVersion string `json:"unicodedata_version"`
}

// sandbox は 1 回の実行の使い捨ての HOME と cwd と driver の置き場。cwd と HOME は別の dir にする。
// tree は stage が本家のファイルを写した新しい木 (stage を通した sandbox だけが持つ)。
type sandbox struct {
	root, cwd, home, drivers string
	tree                     Tree
}

// upstreamMarkers は本家の道具が cwd から上へ辿って拾う名前 (git の最上位の .git・語の一覧・状態の置き場)。
// 使い捨ての dir の上にこれがあると、本家の出力が TMPDIR の置き場所で変わり、記録がそれに汚れる。
var upstreamMarkers = []string{".git", ".yomiyasu-words", ".yomiyasu"}

// markerAbove は dir とその上の dir にある upstreamMarkers の最初のパス。無ければ空。
func markerAbove(dir string) (string, error) {
	for cur := dir; ; {
		for _, name := range upstreamMarkers {
			path := filepath.Join(cur, name)
			if _, err := os.Lstat(path); err == nil {
				return path, nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("%s を調べられません: %w", path, err)
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", nil
		}
		cur = parent
	}
}

func newSandbox() (sandbox, error) {
	root, err := os.MkdirTemp("", "yomiyasu-oracle-")
	if err != nil {
		return sandbox{}, fmt.Errorf("使い捨ての dir を作れません: %w", err)
	}
	// 本家は Path.cwd() (実パス) から上へ辿る。TMPDIR が symlink だと字面の親と実パスの親が別なので、実パスにしてから辿る。
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		_ = os.RemoveAll(root)
		return sandbox{}, fmt.Errorf("使い捨ての dir %s の実パスを引けません: %w", root, err)
	}
	root = real
	if path, err := markerAbove(root); path != "" || err != nil {
		_ = os.RemoveAll(root)
		if err != nil {
			return sandbox{}, err
		}
		return sandbox{}, fmt.Errorf("使い捨ての dir %s の上に %s があります。本家の道具は上へ辿ってこれを拾い、記録の出力が置き場所で変わります。TMPDIR を、上に .git・.yomiyasu-words・.yomiyasu が無い場所に向けてください", root, path)
	}
	sb := sandbox{root: root, cwd: filepath.Join(root, "cwd"), home: filepath.Join(root, "home"), drivers: filepath.Join(root, "drivers")}
	for _, d := range []string{sb.cwd, sb.home, sb.drivers} {
		if err := os.Mkdir(d, 0o755); err != nil {
			_ = os.RemoveAll(root)
			return sandbox{}, fmt.Errorf("%s を作れません: %w", d, err)
		}
	}
	return sb, nil
}

func (s sandbox) remove() { _ = os.RemoveAll(s.root) }

// newStagedSandbox は newSandbox に、検証した本家のファイルの写し (stage) を足したもの。本家の道具を動かす実行はこれを使う。
func (r Runner) newStagedSandbox() (sandbox, error) {
	sb, err := newSandbox()
	if err != nil {
		return sandbox{}, err
	}
	sums, err := Manifest()
	if err == nil {
		sb.tree = Tree{Root: filepath.Join(sb.root, "upstream")}
		err = stageTree(r.Tree, sb.tree, UpstreamFiles, sums)
	}
	if err != nil {
		sb.remove()
		return sandbox{}, err
	}
	return sb, nil
}

// stageTree は files だけを、src から読んだバイト列の SHA-256 が sums と合うことを確かめて dst に書く (1 つでも合わなければ error で、書き終えた分は呼び出し側が消す)。
// src の木をそのまま import させない: SHA-256 を見るのは .py の中身だけで、同じ dir の __pycache__ の .pyc (-B は書かないだけで、有効な .pyc は読む) や
// 一覧に無い .py は検証の外にあり、実行されるものが検証したソースと別になりうる。確かめたバイト列を新しい dir に書けば、そこには検証したファイルしか無い。
func stageTree(src, dst Tree, files []string, sums map[string]string) error {
	for _, rel := range files {
		body, err := os.ReadFile(src.Path(rel))
		if err != nil {
			return fmt.Errorf("本家のファイル %s を読めません (make golden で取り直す): %w", rel, err)
		}
		if got := SHA256Hex(body); got != sums[rel] {
			return fmt.Errorf("本家のファイル %s の SHA-256 が upstream.sha256 と合いません (置き場の %s、取得 %s、期待 %s)", rel, src.Root, got, sums[rel])
		}
		if err := writeFileAtomic(dst.Path(rel), body); err != nil {
			return err
		}
	}
	return nil
}

// env は空から作る環境変数。PYTHONHASHSEED=0 は python3 を -I で動かすと無視される (環境変数を全部読まなくなる) ので、
// -I でなく -P -s -S -B で動かし (pythonFlags を参照)、継承した PYTHON* が無いことは env を空から作ることで保つ。
func (r Runner) env(home string) []string {
	return []string{
		"PATH=" + filepath.Dir(r.Python) + ":/usr/bin:/bin",
		"HOME=" + home,
		"TZ=UTC",
		"PYTHONHASHSEED=" + cmp.Or(r.HashSeed, "0"),
	}
}

// RawResult は 1 回の python3 の実行の結果。rc が 0 でないのは異常ではなく記録の対象で、起動できなかったときだけ error を返す。
type RawResult struct {
	Stdout string
	Stderr string
	RC     int
}

// pythonFlags は python3 の起動の引数。
// -P: cwd や script の dir を sys.path に足さない / -s: user site を読まない / -S: site を import しない (system site の .pth と sitecustomize が
// driver より前に走って unicodedata などを差し替えると、誤った記録と生成表が同時に正解になる) / -B: .pyc を書かない / -X utf8: locale に依らず UTF-8。
// -I にしない (PYTHONHASHSEED も無視される)。本家のファイルの .pyc は stage の写しに無いので読まれない。
var pythonFlags = []string{"-P", "-s", "-S", "-B", "-X", "utf8"}

func (r Runner) exec(ctx context.Context, sb sandbox, stdin []byte, args ...string) (RawResult, error) {
	full := append(slices.Clone(pythonFlags), args...)
	cmd := subproc.CommandContext(ctx, r.Python, full...)
	cmd.Dir = sb.cwd
	cmd.Env = r.env(sb.home)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
	default:
		return RawResult{}, fmt.Errorf("python3 を動かせませんでした (%s): %w", strings.Join(args, " "), err)
	}
	if !utf8.Valid(stdout.Bytes()) || !utf8.Valid(stderr.Bytes()) {
		return RawResult{}, fmt.Errorf("python3 の出力が UTF-8 ではありません (%s)", strings.Join(args, " "))
	}
	return RawResult{Stdout: stdout.String(), Stderr: stderr.String(), RC: cmd.ProcessState.ExitCode()}, nil
}

// runDriver は埋め込みの driver を使い捨ての dir に置き、JSON の指示を標準入力に渡して、標準出力の JSON を out に読む。
func (r Runner) runDriver(ctx context.Context, sb sandbox, name string, in, out any) error {
	src, err := drivers.ReadFile("py/" + name)
	if err != nil {
		return fmt.Errorf("driver %s を読めません: %w", name, err)
	}
	path := filepath.Join(sb.drivers, name)
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return fmt.Errorf("driver %s を置けません: %w", name, err)
	}
	stdin, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("driver %s への指示を JSON にできません: %w", name, err)
	}
	res, err := r.exec(ctx, sb, stdin, path)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("driver %s が rc=%d で終わりました: %s", name, res.RC, res.Stderr)
	}
	if err := json.Unmarshal([]byte(res.Stdout), out); err != nil {
		return fmt.Errorf("driver %s の出力を読めません: %w", name, err)
	}
	return nil
}

// Info は今の python3 のバージョンと unicodedata のバージョンを返す。
func (r Runner) Info(ctx context.Context) (PyInfo, error) {
	sb, err := newSandbox()
	if err != nil {
		return PyInfo{}, err
	}
	defer sb.remove()
	var info PyInfo
	if err := r.runDriver(ctx, sb, "info.py", struct{}{}, &info); err != nil {
		return PyInfo{}, err
	}
	return info, nil
}

// CLISpec は本家の CLI の 1 回の実行。Script は ScriptsDir の中のファイル名、Files は cwd に置く入力 (cwd からの相対パス)。
// 本家の道具は作業フォルダの外のファイルを読まないので、入力は cwd の中に置く。
type CLISpec struct {
	Script string            `json:"script"`
	Args   []string          `json:"args"`
	Stdin  string            `json:"stdin,omitempty"`
	Files  map[string]string `json:"files,omitempty"`
}

// CLIResult は CLI の stdout・stderr・rc。
type CLIResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	RC     int    `json:"rc"`
}

// RunCLI は本家の CLI を cwd の使い捨ての dir で動かして、stdout・stderr・rc を返す。
func (r Runner) RunCLI(ctx context.Context, spec CLISpec) (CLIResult, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return CLIResult{}, err
	}
	defer sb.remove()
	if err := placeFiles(sb.cwd, spec.Files); err != nil {
		return CLIResult{}, err
	}
	res, err := r.exec(ctx, sb, []byte(spec.Stdin), append([]string{sb.tree.Path(ScriptsDir + "/" + spec.Script)}, spec.Args...)...)
	if err != nil {
		return CLIResult{}, err
	}
	return CLIResult(res), nil
}

// placeFiles は files を dir の中に置く。dir の外へ出るパスは拒否する。
func placeFiles(dir string, files map[string]string) error {
	for rel, body := range files {
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("cwd の外へ出るパスは置けません: %q", rel)
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("%s の dir を作れません: %w", dst, err)
		}
		if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
			return fmt.Errorf("%s を置けません: %w", dst, err)
		}
	}
	return nil
}

// Call は本家の module の関数 1 つの呼び出し。引数は JSON で表せるものだけ (Ref・Bytes・Cps で、前の結果・bytes・単独のサロゲートを含む str も渡せる)。
// 1074 で足した欄 (どれも空なら前と同じ): Get は関数を呼ばずに module の属性 (定数) を返す・Save は戻り値を覚える (Ref で渡す)・Digest は結果を SHA-256 の先頭 8 字にして返す
// (Keep も付けると結果も添える)・Op は関数でなく、作業フォルダへの操作 (fixture を作る・環境を変える)。
// 1075 で足した欄: Quiet は Digest と一緒に付け、結果の digest を 8 つの 0 にする (analysis のように、覚える値を作るだけで結果の記録は要らない呼び出し)。
type Call struct {
	Module   string         `json:"module"`
	Function string         `json:"function"`
	Args     []any          `json:"args,omitempty"`
	Kwargs   map[string]any `json:"kwargs,omitempty"`
	Get      bool           `json:"get,omitempty"`
	Save     string         `json:"save,omitempty"`
	Digest   bool           `json:"digest,omitempty"`
	Keep     bool           `json:"keep,omitempty"`
	Quiet    bool           `json:"quiet,omitempty"`
	ArgsOut  bool           `json:"args_out,omitempty"`
	// ArgsOutOnly は結果を [戻り値, 呼んだ後の args のうち、この添字のもの] にする (nil は付けない。args に JSON にできない値があるとき用)。
	ArgsOutOnly []int `json:"args_out_only,omitempty"`
	Op          *Op   `json:"op,omitempty"`
}

// PyException は本家の関数が投げた例外。Type が "Unserializable" のときは戻り値が JSON にできなかった。
type PyException struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// CallResult は Call の結果。Exception が nil のとき Result が戻り値の JSON。Digest を求めた呼び出しは Digest (と、例外のときの Raised) が入り、
// Keep も付けたときだけ Result か Exception も入る。
type CallResult struct {
	Result    json.RawMessage `json:"result,omitempty"`
	Exception *PyException    `json:"exception,omitempty"`
	Digest    string          `json:"digest,omitempty"`
	Raised    bool            `json:"raised,omitempty"`
}

// CallFunctions は calls を 1 つの Python プロセスの中で順に呼ぶ (呼び出しごとに起動しない)。結果は calls と同じ順。
func (r Runner) CallFunctions(ctx context.Context, calls []Call) ([]CallResult, error) {
	sb, err := r.newStagedSandbox()
	if err != nil {
		return nil, err
	}
	defer sb.remove()
	var out []CallResult
	git, gitEnv := "", GitEnv("")
	if needsGit(calls) {
		if git, err = exec.LookPath("git"); err != nil {
			return nil, fmt.Errorf("git が見つかりません (fixture を作る op が要ります): %w", err)
		}
		if git, err = filepath.Abs(git); err != nil {
			return nil, err
		}
		gitEnv = GitEnv(git)
	}
	in := struct {
		ScriptsDir string            `json:"scripts_dir"`
		Calls      []Call            `json:"calls"`
		Git        string            `json:"git"`
		GitEnv     map[string]string `json:"git_env"`
	}{sb.tree.Path(ScriptsDir), calls, git, gitEnv}
	if err := r.runDriver(ctx, sb, "call_function.py", in, &out); err != nil {
		return nil, err
	}
	if len(out) != len(calls) {
		return nil, fmt.Errorf("関数の呼び出し %d 件に対して結果が %d 件でした", len(calls), len(out))
	}
	return out, nil
}
