package issues

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"termsafe"
)

// 「次にやる」の目印 (next/) を symlink で表す層 (issue 263)。
//
// 以前は issue ファイルそのものを `next/` へ rename していた。それだと本文の相対リンク
// (`done/549-x.md` のように issue ディレクトリ直下を起点に書かれる) が 1 段下がった時点で全部切れ、
// pre-push のリンク検査に落ちる (obaket で実測 2 回)。リンクを書き換えると claim という状態変更の
// commit に本文の差分が混ざる。そこで **ファイルは動かさず、`next/<base>` に `../<base>` を指す
// symlink を置く**。Path (同一性) も参照も安定し、claim / 解除は symlink の作成 / 削除だけになる。
//
// 🚨 symlink を「読む」例外はここだけに閉じる。isIssueFile は symlink を弾く (PR に
// `issues/999.md -> ~/.ssh/id_rsa` を入れられると中身が画面に出る。discover.go の注記) ので、
// 目印として採用する条件を狭く固定する — 全部満たすものだけ:
//
//   - `<parent>/next/<base>` が Lstat で symlink (通常ファイルは従来の rename 運用として別扱い)
//   - Readlink の値が **ちょうど** `../<base>` (相対・1 段上・同名)。絶対パス、別名、`..` の連鎖、
//     `./` 混じりはすべて不採用。clone 先でも成立する形はこれ 1 つで、他を受ける理由が無い
//   - `<parent>/<base>` が Lstat で通常ファイル (symlink の連鎖は不採用)
//
// 採用したときも **本文は `<parent>/<base>` から読む** (symlink を経由しない)。不採用の symlink は
// 黙って捨てず warnings に出す: 目印が壊れている (先の issue が done/ へ動いた等) のは運用の異常で、
// 見えないと「claim したはずの issue が [next] に無い」だけが残る。

// nextLinks は parent/next/ にある目印 symlink を読み、採用したものを base → symlink の絶対パスで返す。
// 採用しなかった symlink は warnings (表示用に無害化済み) にする。next/ が無ければ空。
func nextLinks(parent string) (marked map[string]string, warnings []string) {
	nextDir := filepath.Join(parent, NextDirName)
	// 🚨 next/ 自体が symlink なら丸ごと不採用 (hasMarkdown のディレクトリ symlink 拒否と同じ方針)。
	// 追うと `issues/next -> /tmp/evil` の中身で目印を捏造でき、解除の Remove が repo 外を消す先になる
	if fi, err := os.Lstat(nextDir); err != nil {
		return nil, nil
	} else if fi.Mode()&os.ModeSymlink != 0 {
		return nil, []string{"next の目印を無視しました (next/ 自体が symlink): " +
			termsafe.PlainLine(filepath.Join(filepath.Base(parent), NextDirName))}
	}
	entries, err := os.ReadDir(nextDir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 || !isMarkdown(e.Name()) || metaFiles[strings.ToLower(e.Name())] {
			// 通常ファイルは scanDir / scanEpicDir が旧運用として読む。metaFiles (README.md 等) は
			// 直下でも issue として数えないので、目印にもしない (数えると照合相手が無く偽警告になる)
			continue
		}
		link := filepath.Join(nextDir, e.Name())
		if reason := nextLinkProblem(parent, e.Name()); reason != "" {
			warnings = append(warnings, "next の目印を無視しました ("+reason+"): "+
				termsafe.PlainLine(filepath.Join(filepath.Base(parent), NextDirName, e.Name())))
			continue
		}
		if marked == nil {
			marked = make(map[string]string, 4)
		}
		marked[e.Name()] = link
	}
	return marked, warnings
}

// unmatchedNextLinks は走査で直下の issue と突き合わせられなかった目印を警告にする。
// 採用条件を通っても、大文字小文字や正規化の違い (APFS で人が手で張った目印) で直下のエントリ名と
// 完全一致しないことがある。黙って捨てると「壊れた目印は黙って捨てない」の唯一の穴になる。
func unmatchedNextLinks(parent string, marked map[string]string) []string {
	warns := make([]string, 0, len(marked))
	for base := range marked {
		warns = append(warns, "next の目印を無視しました (直下に同じ名前の issue が無い): "+
			termsafe.PlainLine(filepath.Join(filepath.Base(parent), NextDirName, base)))
	}
	sort.Strings(warns)
	return warns
}

// nextLinkProblem は目印 symlink が採用条件を満たさない理由 ("" = 採用)。
func nextLinkProblem(parent, base string) string {
	target, err := os.Readlink(filepath.Join(parent, NextDirName, base))
	if err != nil {
		return "読めない"
	}
	if target != "../"+base {
		return "指す先が ../<同名> でない"
	}
	fi, err := os.Lstat(filepath.Join(parent, base))
	if err != nil {
		return "指す先の issue が無い"
	}
	if !fi.Mode().IsRegular() {
		return "指す先が通常ファイルでない"
	}
	return ""
}

// NextLinkPath は iss に目印を置くときの symlink の絶対パス (`<parent>/next/<base>`)。
// parent は global issue なら Dir、Epic の子なら GroupKey。
func NextLinkPath(iss *Issue) string {
	parent := iss.Dir
	if iss.GroupKind == GroupEpic && iss.GroupKey != "" {
		parent = iss.GroupKey
	}
	return filepath.Join(parent, NextDirName, filepath.Base(iss.Rel))
}

// isOpenPlacement は iss が「直下」(global は Dir 直下、Epic の子は group 直下) にあるか。
// 目印を symlink で置けるのはこの配置だけ (../<base> が成立する)。
func isOpenPlacement(iss *Issue) bool {
	rel := iss.Rel
	if iss.GroupKind == GroupEpic && iss.GroupKey != "" {
		r, err := filepath.Rel(iss.GroupKey, iss.Path)
		if err != nil {
			return false
		}
		rel = r
	}
	return !strings.Contains(rel, string(filepath.Separator))
}
