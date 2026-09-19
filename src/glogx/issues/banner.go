package issues

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"glogx/atomicfile"
)

// claim の担当者バナー (issue 本文冒頭の `> 🚨 **担当中: <誰>**（YYYY-MM-DD〜）`)。
//
// なぜ viewer が書くか: next/ の目印は next/ を見る入口にしか届かないので、claim ルール
// (_claude/rules/claim-issue-in-next-and-push.md) は本文冒頭にもバナーを要求し、CI
// (tests/issues/test_next_claims_have_banner.sh) がバナーの無い claim を落とす。`n` が目印だけを
// 置くと、`n` で付けた claim は必ず CI で赤になる (dotfiles issue 403 の残タスク)。
//
// 🚨 判定 (bannerLine / fenceLine) は CI 側の awk と同じ規則の Go 版。言語が違うので 1 実装に
// 寄せられない。変えるなら両方を同じ commit で変え、両方のテストを通す:
//   - 最初の `## ` 見出しより前だけを見る
//   - コードフェンス (字下げ 3 桁までの ``` / ~~~) の中は見ない
//   - 行頭 (前に `> ` と `🚨 ` を許す) の `**担当中` / `**着手中`
//
// どれも行頭の一致なので CRLF の行末は判定に影響しない (\r を落とす処理は要らない)
var (
	bannerLine = regexp.MustCompile(`^(> )?(🚨 )?\*\*(担当中|着手中)`)
	fenceLine  = regexp.MustCompile("^ {0,3}(```|~~~)")
)

// bannerIndex は本文冒頭のバナー行の添字を返す (無ければ -1)。
func bannerIndex(lines []string) int {
	fence := false
	for i, l := range lines {
		if fenceLine.MatchString(l) {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if strings.HasPrefix(l, "## ") {
			return -1
		}
		if bannerLine.MatchString(l) {
			return i
		}
	}
	return -1
}

// ClaimBanner は viewer が書くバナー行。誰の claim かはホスト名で表す (glogx を操作するのは人で、
// claim が防ぎたいのは別マシンとの二重着手なので、マシンが識別子として足りる)。
func ClaimBanner(now time.Time) string {
	who, err := os.Hostname()
	if err != nil || who == "" {
		who = "不明なホスト"
	}
	who, _, _ = strings.Cut(who, ".") // koji-mbp.local → koji-mbp
	return fmt.Sprintf("> 🚨 **担当中: %s (glogx)**（%s〜）", who, now.Format("2006-01-02"))
}

// addBanner は本文冒頭にバナーを差し込んだ内容を返す。既にあれば変更なし (changed=false)。
// 形は常に「H1・空行・バナー・空行・本文」(H1 が無ければ「バナー・空行・本文」)。H1 と本文の間の
// 空行は 1 つにまとめる。removeBanner と対で、標準形 (H1 の後に空行 1 つ) は往復で元に戻る。
func addBanner(src, banner string) (string, bool) {
	lines := strings.Split(src, "\n")
	if bannerIndex(lines) >= 0 {
		return src, false
	}
	var head []string
	rest := lines
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		head, rest = []string{lines[0], ""}, lines[1:]
	}
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	out := append(append(head, banner, ""), rest...)
	return strings.Join(out, "\n"), true
}

// removeBanner は本文冒頭のバナー行と、その直後の空行 1 つを取り除く。無ければ変更なし。
// 🚨 viewer が書いたものに限らず冒頭のバナーを外す: claim を外した issue に「担当中」が残ると、
// 次に開いた人が誰かの作業中と読んで着手を控える (古いバナー)。
func removeBanner(src string) (string, bool) {
	lines := strings.Split(src, "\n")
	i := bannerIndex(lines)
	if i < 0 {
		return src, false
	}
	j := i + 1
	if j < len(lines) && strings.TrimSpace(lines[j]) == "" {
		j++
	}
	out := append(append([]string{}, lines[:i]...), lines[j:]...)
	return strings.Join(out, "\n"), true
}

// rewriteIssue は path の本文を f で書き換える。mode は保つ。変更が無ければ書かない。
//
// 🚨 temp + rename なので、path が symlink なら「リンクが通常ファイルに置き換わり、リンク先は古いまま」、
// ハードリンクなら「リンクが切れる」。どちらも黙って壊すより拒否する (xattr・所有者も temp + rename では
// 保たれないが、issue ファイルに付く運用は無いので扱わない)。
//
// 🚨 読んでから書くまでの間にエディタが保存すると、その編集を上書きで消す。rename の直前に mtime と
// サイズを取り直し、変わっていれば中止する。**窓は縮むだけで 0 にはならない** (取り直しから rename の
// 間は残る)。開いているエディタのバッファも inode の差し替えで外れる (再読込が要る)。
func rewriteIssue(path string, f func(string) (string, bool)) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s は symlink なので本文を書き換えません", filepath.Base(path))
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		return fmt.Errorf("%s はハードリンクなので本文を書き換えません", filepath.Base(path))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, changed := f(string(b))
	if !changed {
		return nil
	}
	beforeRewrite(path)
	now, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !now.ModTime().Equal(fi.ModTime()) || now.Size() != fi.Size() {
		return fmt.Errorf("%s が書き換え中に変更されたので中止しました (もう一度実行してください)", filepath.Base(path))
	}
	return atomicfile.Write(path, []byte(out), fi.Mode().Perm())
}

// beforeRewrite はテストの差し替え口 (読んでから書くまでの間の並行編集を再現する)。
var beforeRewrite = func(string) {}

// bannerNow は時刻の差し替え口 (テストで日付を固定する)。
var bannerNow = time.Now

func writeClaimBanner(path string) error {
	banner := ClaimBanner(bannerNow())
	return rewriteIssue(path, func(s string) (string, bool) { return addBanner(s, banner) })
}

func clearClaimBanner(path string) error {
	return rewriteIssue(path, removeBanner)
}
