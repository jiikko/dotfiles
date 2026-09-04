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

// brewAction は 1 つの手。Cmd はそのまま貼れる形にする。
type brewAction struct {
	Label string // 日本語のラベル (何をする手か)
	Cmd   string // 実行するコマンド
	Note  string // 打つ前に知っておくこと ("" = 無し)
}

// brewAdvice は警告 1 件の訳と手。Known=false なら未知のパターン (英語のまま出す)。
type brewAdvice struct {
	Known   bool
	Title   string // 日本語の見出し
	Detail  string // 何が起きているか / 放っておくとどうなるか
	Actions []brewAction
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

// brewNamesAfter は marker 行より後ろの行から名前を集める。
//
// 行の形は 2 つある: インデントされた名前だけの行 (`  node`) と、brew が出す
// コマンド行 (`  brew install a b c`)。後者は先頭の `brew <sub>` を落として残りを名前として読む。
// 空行で打ち切らない (brew は名前の列のあとに別の段落を続けることがある) が、
// **次の `Warning:` や別の marker が来たら止める**。
func brewNamesAfter(body, marker string) (names []string, dropped int) {
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		return nil, 0
	}
	for _, line := range strings.Split(rest, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Warning:") {
			break
		}
		fields := strings.Fields(t)
		// brew が出したコマンド行はサブコマンドまでを落とす (`brew install a b` → a b)
		if len(fields) >= 2 && fields[0] == "brew" {
			fields = fields[2:]
		}
		// 名前以外の散文が来たら打ち切る (「You should ...」等)
		allName := len(fields) > 0
		for _, f := range fields {
			if !safeBrewName(f) {
				allName = false
				break
			}
		}
		if !allName {
			// 1 行まるごと名前でないなら、その行は名前の列ではない。
			// ただし一部だけ壊れている可能性があるので、名前として通る語だけ拾って残りを数える
			got := 0
			for _, f := range fields {
				if safeBrewName(f) {
					names = append(names, f)
					got++
				}
			}
			if got == 0 {
				break // 名前が 1 つも無い散文 = 列の終わり
			}
			dropped += len(fields) - got
			continue
		}
		names = append(names, fields...)
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
			a.Actions = append(a.Actions, brewAction{Label: "リンクを張り直す", Cmd: "brew link " + join(ns)})
			a.Actions = append(a.Actions, brewAction{
				Label: "使っていないなら削除する", Cmd: "brew uninstall " + join(ns),
				Note: "他の formula が依存していると壊れます。先に brew uses --installed で確認してください"})
		}
		return a

	case strings.Contains(head, "deprecated or disabled"):
		ns, dropped := brewNamesAfter(warning, "replacements for the following formulae:")
		a := brewAdvice{Known: true, Dropped: dropped,
			Title:  "非推奨 / 無効になった formula があります",
			Detail: "今後は更新されず、いずれ入らなくなります。代替に乗り換えるか、使っていないなら削除します。"}
		if len(ns) > 0 {
			a.Detail += "\n対象: " + join(ns)
			a.Actions = append(a.Actions,
				brewAction{Label: "代替と非推奨の理由を調べる", Cmd: "brew info " + join(ns)},
				brewAction{Label: "使っていないなら削除する", Cmd: "brew uninstall " + join(ns),
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
				brewAction{Label: "足りない依存を入れる", Cmd: "brew install " + join(ns)},
				brewAction{Label: "どれが欠けているか確認する", Cmd: "brew missing"})
		}
		return a
	}
	return brewAdvice{}
}
