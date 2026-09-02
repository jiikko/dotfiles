package svc

import (
	"context"
	"strings"

	"doctor/brewledger"
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

// brewFormulae は Homebrew の台帳 (doctor/brewledger。旧名・別名を含む)。brew が無い / 失敗したら error で、
// C は評価しない (その旨を表示する。候補 0 件には畳まない)。
func brewFormulae(ctx context.Context, run runner.Runner) (map[string]bool, error) {
	return brewledger.Installed(ctx, run)
}
