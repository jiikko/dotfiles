package wtclean

// tmp/ の退避 (issue 552)。設定の disposable_tmp に書いた repo では、worktree の直下の tmp/ の無視されたファイル (見本・変異のスクリプト・
// 計測のログ) だけを理由に worktree を残さず、状態の置き場の wtclean-tmp/<repo>/<名前>/ へ移してから消す。
// 🚨 退避で代えているのは CLAUDE.md の「tmp/ を消す前に issue や doc が指しているパスでないか確かめる」(機械では確かめられない)。
// 指していたと後で分かっても、TmpKeep の間は退避から戻せる。TmpKeep を過ぎた退避は片付け (PruneTmpStash) のたびに消す。
// 退避の後に worktree を消せなければ、移したファイルを元へ戻す (worktree が残るなら中身も元のまま)。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TmpStashDir は状態の置き場の下の退避の置き場 (<状態の置き場>/wtclean-tmp/<repo>/<worktree の名前>/tmp/...)。
const TmpStashDir = "wtclean-tmp"

// TmpKeep は退避を残す長さ (make clean-tmp の既定の 30 日と同じ)。
const TmpKeep = 30 * 24 * time.Hour

// tmpNote は判定の理由に足す、退避するファイルの数 (無ければ空)。
func tmpNote(tmp []string) string {
	if len(tmp) == 0 {
		return ""
	}
	return fmt.Sprintf("。tmp/ の無視されたファイル %d 件は %s/<repo>/<名前>/ へ退避してから消す", len(tmp), TmpStashDir)
}

// stashedNote は消した結果に足す、退避した先。
func stashedNote(n int, dest string) string {
	if dest == "" {
		return ""
	}
	return fmt.Sprintf("。tmp/ の %d 件を %s へ退避した (%d 日後の片付けで消す)", n, dest, int(TmpKeep/(24*time.Hour)))
}

// removeTree は worktree を外す (removeWorktree)。v.Tmp があれば先に退避し、退避の後に tmp/ の外にも無視されたファイルが増えていないかを
// 読み直してから外す。外せなければ退避したファイルを戻す。返すのは退避した先 (退避していなければ空)。
func removeTree(ctx context.Context, v Verdict, stateDir string) (string, error) {
	if len(v.Tmp) == 0 {
		return "", removeWorktree(ctx, v)
	}
	dest, undo, err := stashTmp(v, stateDir)
	if err != nil {
		return "", fmt.Errorf("tmp/ を退避できないので消さない: %w", err)
	}
	fail := func(err error) (string, error) {
		if uerr := undo(); uerr != nil {
			return "", fmt.Errorf("%w (退避した tmp/ を戻せない。%s に残っている: %w)", err, dest, uerr)
		}
		return "", err
	}
	// 判定し直した後にできたファイル (git worktree remove が黙って消す) を、退避の後に読み直して拾う
	st, err := readStatus(ctx, v.Path, true)
	switch {
	case err != nil:
		return fail(fmt.Errorf("退避の後に git status を読めない: %w", err))
	case len(st.tmp)+len(st.ignored) > 0:
		return fail(fmt.Errorf("退避の間に無視されたファイルができた (%s)", clip(append(st.tmp, st.ignored...))))
	}
	if err := removeWorktree(ctx, v); err != nil {
		return fail(err)
	}
	return dest, nil
}

// stashTmp は v.Tmp を <stateDir>/wtclean-tmp/<repo>/<名前>/ へ移す (同じ名前の退避が既にあれば <名前>.2 …)。
// undo は移したファイルを元へ戻し、空になった退避を消す (元の場所に同じ名前のファイルができていれば上書きせず、退避に残して失敗を返す)。
// 途中で移せなければ、移した分を戻してから失敗を返す。
func stashTmp(v Verdict, stateDir string) (string, func() error, error) {
	if stateDir == "" {
		return "", nil, errors.New("状態の置き場が無い")
	}
	parent := filepath.Join(stateDir, TmpStashDir, v.Repo)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", nil, err
	}
	dest := ""
	for i := 1; i <= 100 && dest == ""; i++ {
		d := filepath.Join(parent, v.Name)
		if i > 1 {
			d += "." + strconv.Itoa(i)
		}
		switch err := os.Mkdir(d, 0o755); {
		case err == nil:
			dest = d
		case !errors.Is(err, os.ErrExist):
			return "", nil, err
		}
	}
	if dest == "" {
		return "", nil, fmt.Errorf("%s の下に %s の退避が 100 個ある", parent, v.Name)
	}
	var moved []string
	undo := func() error {
		var errs []error
		for i := len(moved) - 1; i >= 0; i-- {
			src, dst := filepath.Join(dest, moved[i]), filepath.Join(v.Path, moved[i])
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				errs = append(errs, err)
				continue
			}
			if _, err := os.Lstat(dst); err == nil { // 退避の間に同じ名前でできたファイルを上書きしない (こちらは退避に残す)
				errs = append(errs, fmt.Errorf("%s が退避の間にできている (上書きしない)", dst))
				continue
			}
			if err := os.Rename(src, dst); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		left, err := filesUnder(dest) // 戻し切れていなければ退避を消さない
		switch {
		case err != nil:
			return fmt.Errorf("戻した後の退避を読めない: %w", err)
		case len(left) > 0:
			return fmt.Errorf("退避に %d 件残っている", len(left))
		}
		return os.RemoveAll(dest)
	}
	for _, rel := range v.Tmp {
		src, dst := filepath.Join(v.Path, rel), filepath.Join(dest, rel)
		err := os.MkdirAll(filepath.Dir(dst), 0o755)
		if err == nil {
			err = os.Rename(src, dst)
		}
		if err != nil {
			if uerr := undo(); uerr != nil {
				return "", nil, fmt.Errorf("%w (移した分を戻せない。%s に残っている: %w)", err, dest, uerr)
			}
			return "", nil, err
		}
		moved = append(moved, rel)
	}
	return dest, undo, nil
}

// PruneTmpStash は <stateDir>/wtclean-tmp/<repo>/ の下の退避のうち、作ってから TmpKeep を過ぎたもの (退避のディレクトリの mtime で見る) を消し、
// 消したパスを返す。空になった <repo> のディレクトリも消す。
// 🚨 テストの二進 (testing.Testing) では、SetTestSandbox で登録した置き場の外を拒否する (allow と同じ)。
func PruneTmpStash(stateDir string, now time.Time) ([]string, error) {
	if stateDir == "" {
		return nil, nil
	}
	root := filepath.Join(stateDir, TmpStashDir)
	if testing.Testing() {
		if sb := testSandboxRoot(); sb == "" || !within(root, sb) {
			return nil, fmt.Errorf("テストの二進で、置き場 (%q) の外 (%s) を消そうとした", sb, root)
		}
	}
	repos, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var removed []string
	var errs []error
	for _, r := range repos {
		if !r.IsDir() {
			continue
		}
		dir := filepath.Join(root, r.Name())
		ents, err := os.ReadDir(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		left := len(ents)
		for _, e := range ents {
			fi, err := e.Info()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if now.Sub(fi.ModTime()) < TmpKeep {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if err := os.RemoveAll(p); err != nil {
				errs = append(errs, err)
				continue
			}
			removed = append(removed, p)
			left--
		}
		if left == 0 {
			_ = os.Remove(dir) // 空でなければ消えない (その間に退避が増えた)
		}
	}
	return removed, errors.Join(errs...)
}
