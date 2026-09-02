package svc

import (
	"context"
	"fmt"
	"strings"

	"doctor/runner"
)

// brewLabelPrefix は Homebrew services が置く plist のラベル接頭辞。C の判定はここに限定する:
// 「台帳に無い = 壊れている」が成立するのはパッケージマネージャ経由の登録だけで、手で置いた
// plist に一般化すると偽陽性になる。
const brewLabelPrefix = "homebrew.mxcl."

// brewFormulaOf はラベルから formula 名を取り出す (brew の登録でなければ "")。
func brewFormulaOf(label string) string {
	if !strings.HasPrefix(label, brewLabelPrefix) {
		return ""
	}
	return strings.TrimPrefix(label, brewLabelPrefix)
}

// brewFormulae は `brew list --formula` の台帳。brew が無い / 失敗したら error で、C は評価しない
// (その旨を表示する。候補 0 件には畳まない)。
func brewFormulae(ctx context.Context, run runner.Runner) (map[string]bool, error) {
	out, stderr, rc, err := runner.WithTimeout(ctx, run, launchctlTimeout, "brew", "list", "--formula")
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("brew list --formula: exit %d: %s", rc, strings.TrimSpace(stderr))
	}
	set := map[string]bool{}
	for _, f := range strings.Fields(out) {
		set[f] = true
	}
	return set, nil
}
