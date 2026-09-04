package main

import "strings"

// brew doctor の警告を「日本語の説明」と「打つべきコマンド」に落とす。
//
// なぜ: brew doctor の出力は英語で、しかも「何が問題か」と「どうすればよいか」が
// 同じ段落に混ざっている。パターンごとにやることはほぼ決まっているので、
// 読んで訳す作業を毎回やらせない (ユーザー要望 2026-09-04)。
//
// 🚨 **コマンドは brew の出力行をそのまま使わない。** formula 名だけを取り出し、
// 1 つずつ allowlist で検証してから組み直す。brew の出力は外部由来の文字列で、
// そのままコマンドとして提示すると、細工された名前 (空白・引用符・改行・制御文字) が
// 「コピーして貼る」経路に載る (issue 178 が svc 側で塞いだのと同じ形)。
// 検証に落ちた名前は捨て、その事実を残す (黙って減らさない)。

// brewAdvice は警告 1 件の訳と手。Known=false なら未知のパターン (英語のまま出す)。
type brewAdvice struct {
	Known   bool
	Title   string // 日本語の見出し
	Detail  string // 何が起きているか / 放っておくとどうなるか
	Actions []doctorCmdAction
	Dropped int // allowlist に落ちた名前の数 (0 でなければ画面に出す)
}

// safeBrewName は formula / cask 名として受け入れてよい形か。
// brew の命名で実際に使われる文字だけを通す (英数字と @ + . _ -)。
func safeBrewName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '@' || r == '+' || r == '.' || r == '_' || r == '-' || r == '/':
			// / は tap 付きの名前 (homebrew/core/foo) で出る
		default:
			return false
		}
	}
	// ".." を含む名前は受けない (tap 付きの名前と組み合わせると経路の細工になりうる)
	return !strings.Contains(s, "..")
}

// brewNamesAfter は marker 行より後ろの「名前の列」を集める。
//
// 🚨 **名前の列は「インデントされた連続する行」に限る。** 空行や非インデント行が来たら止める。
// brew は名前の列のあとに散文を続けることがあり (`Uninstall them with ...` / `See also: ...`)、
// 語だけ見て拾うと **散文の単語が formula 名として混ざる** (実際に cask 版の出力で起きた)。
//
// 行の形は 2 つ: 名前だけの行 (`  node`) と、brew が出すコマンド行 (`  brew install a b`)。
// 後者は先頭の `brew <sub>` を落として残りを名前として読む。
// 名前として読めない語が 1 つでもある行は**その行ごと捨てて打ち切る** (細工された名前を
// 部分的に採用しない)。捨てた語数は dropped に数えて画面へ出す。
func brewNamesAfter(body, marker string) (names []string, dropped int) {
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		return nil, 0
	}
	started := false
	for _, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "" {
			if started {
				break // 名前の列は連続する
			}
			continue // marker 直後の空行は読み飛ばす
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break // インデントされていない = 名前の列ではない
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "brew" {
			fields = fields[2:] // brew が出したコマンド行
		}
		allName := len(fields) > 0
		for _, f := range fields {
			if !safeBrewName(f) {
				allName = false
				break
			}
		}
		if !allName {
			dropped += len(fields)
			break
		}
		names = append(names, fields...)
		started = true
	}
	return names, dropped
}

// brewAdviceFor は警告 1 件 (Warning: で始まる塊) を訳と手に落とす。
func brewAdviceFor(warning string) brewAdvice {
	head := strings.SplitN(warning, "\n", 2)[0]
	join := func(ns []string) string { return strings.Join(ns, " ") }

	switch {
	case strings.Contains(head, "unlinked kegs"):
		ns, dropped := brewNamesAfter(warning, "brew link` on these:")
		a := brewAdvice{Known: true, Dropped: dropped,
			Title:  "リンクされていない keg があります",
			Detail: "インストール済みだが symlink が張られていない状態です。これに依存する formula は、ビルドは通っても実行時に壊れることがあります。"}
		if len(ns) > 0 {
			a.Detail += "\n対象: " + join(ns)
			a.Actions = append(a.Actions, doctorCmdAction{Label: "リンクを張り直す", Cmd: "brew link " + join(ns)})
			a.Actions = append(a.Actions, doctorCmdAction{
				Label: "使っていないなら削除する", Cmd: "brew uninstall " + join(ns),
				Note: "他の formula が依存していると壊れます。先に brew uses --installed で確認してください"})
		}
		return a

	case strings.Contains(head, "deprecated or disabled"):
		// 🚨 cask 版と formula 版で marker も削除コマンドも違う (--cask が要る)。
		// 見出しだけ合わせて marker を formula 固定にすると、cask では名前が 1 つも取れず
		// 「日本語の見出しになったのに手が 1 つも出ない」形になる (実測 2026-09-04)
		cask := strings.Contains(head, "casks")
		var ns []string
		var dropped int
		for _, m := range []string{
			"replacements for the following formulae:",
			"replacements for the following casks:",
			"find replacements:",
		} {
			if ns, dropped = brewNamesAfter(warning, m); len(ns) > 0 {
				break
			}
		}
		kind, uninstall := "formula", "brew uninstall "
		if cask {
			kind, uninstall = "cask", "brew uninstall --cask "
		}
		a := brewAdvice{Known: true, Dropped: dropped,
			Title:  "非推奨 / 無効になった " + kind + " があります",
			Detail: "今後は更新されず、いずれ入らなくなります。代替に乗り換えるか、使っていないなら削除します。"}
		if len(ns) > 0 {
			a.Detail += "\n対象: " + join(ns)
			a.Actions = append(a.Actions,
				doctorCmdAction{Label: "代替と非推奨の理由を調べる", Cmd: "brew info " + join(ns)},
				doctorCmdAction{Label: "使っていないなら削除する", Cmd: uninstall + join(ns),
					Note: "代替を決めてから。依存元は brew uses --installed で確認できます"})
		}
		return a

	case strings.Contains(head, "missing dependencies"):
		ns, dropped := brewNamesAfter(warning, "the missing dependencies:")
		a := brewAdvice{Known: true, Dropped: dropped,
			Title:  "依存が欠けている formula / cask があります",
			Detail: "依存先が削除されたか、インストールが途中で終わっています。足りないものを入れれば直ります。"}
		if len(ns) > 0 {
			a.Actions = append(a.Actions,
				doctorCmdAction{Label: "足りない依存を入れる", Cmd: "brew install " + join(ns)},
				doctorCmdAction{Label: "どれが欠けているか確認する", Cmd: "brew missing"})
		}
		return a
	}
	return brewAdvice{}
}
