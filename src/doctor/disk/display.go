package disk

// 表示・コピーに出す前の関門 (issue 228)。
//
// 🚨 **走査した値は「自分以外が書いた文字列」**。パスはファイル名由来 (macOS のファイル名は
// `/` と NUL 以外の任意バイトを取れる)、Contents は `os.ReadDir` の名前、Reason / Failures は
// OS のエラー文。カタログの対象は `$TMPDIR/…` / `~/Library/Caches/…` のような**ユーザーが
// 書き込める領域**なので、細工した名前のディレクトリを置くだけで注入できる。
//
// これらが無害化されずに端末へ出ると:
//
//   - CLI (diskdoctor) は **stdout へ直接書く**ので、OSC52 (クリップボード書き込み) や
//     タイトル書き換え・画面消去が「表示しただけ」で発火する
//   - TUI (glogx) の描画層はセル単位に分解するので端末制御そのものは落ちるが、**改行で偽の行**が
//     作れ (固定高パネルの行数が狂う)、SGR が次の行へ滲み、OSC8 のリンクは素通りする。
//     さらに `y` / `Y` のコピーは pbcopy へ生で渡るので、**貼った先の端末**で発火する
//
// 関門を出所ごとに書き分けると必ずどこかが漏れる (実際 doctor の live 経路だけが 1 度も
// 通っていなかった) ので、**CLI の Format と glogx の受け口が同じこの関数を通す**。
//
// 🚨 ここで落とすのは**制御文字だけ**。長さ・件数の上限は「保存されたキャッシュを読み戻す」
// 側の関心 (画面を埋め尽くさせない) なので、そちらに置いたままにする。

import (
	"fmt"
	"unicode"

	"termsafe"
)

// SanitizeForDisplay は Report を表示・コピーに出してよい形にする (Total は引き直す)。
func SanitizeForDisplay(rep Report) Report {
	out := rep
	out.Results = make([]Result, 0, len(rep.Results))
	for _, r := range rep.Results {
		out.Results = append(out.Results, SanitizeResultForDisplay(r))
	}
	out.Total = SumDeletable(out.Results)
	return out
}

// SanitizeResultForDisplay は 1 エントリ分を無害化する。
//
// 🚨 **Item.Path は書き換えずに「落とす」**。書き換えると画面に出るパスと実体が食い違い、
// 削除の照合 (planDelete が itemKey で突き合わせる) から外れる = 「見えているものと消える
// ものが違う」を作る。落とした件数は Failures に残して、合計から引く (黙って減らさない)。
// 保存経路 (glogx の sanitizeSnapshotResults) が元々この形なので、live もそちらへ揃える。
func SanitizeResultForDisplay(r Result) Result {
	r.Entry = SanitizeEntryForDisplay(r.Entry)
	r.Reason = termsafe.PlainLine(r.Reason)
	r.Failures = sanitizeLines(r.Failures)
	r.Contents = sanitizeLines(r.Contents)

	kept := make([]Item, 0, len(r.Items))
	dropped := 0
	for _, it := range r.Items {
		if !DisplayablePath(it.Path) {
			dropped++
			continue
		}
		it.Ref = termsafe.PlainLine(it.Ref)
		kept = append(kept, it)
	}
	if dropped > 0 {
		var sum int64
		for _, it := range kept {
			sum += it.Size
		}
		r.Items, r.Size = kept, sum
		r.Failures = append(r.Failures,
			fmt.Sprintf("%d 件は名前に制御文字を含むため一覧から外しました (合計にも含めていません)", dropped))
	}
	return r
}

// DisplayablePath は「画面とコピーに出してよいパスか」。
//
// 🚨 termsafe を通して**書き換える**のではなく、通らないものを落とすための述語なのでここだけ
// 自前で判定する。パスは表示だけでなく削除の照合にも使う値で、書き換えたら別物になる。
// 🚨 `unicode.IsPrint` はタブも改行も非印字として弾く (どちらも 1 行 1 件の契約を壊す)。
func DisplayablePath(p string) bool {
	if p == "" {
		return false
	}
	for _, r := range p {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// SanitizeEntryForDisplay はカタログ由来の表示文字列を通す。
//
// 走査した Result の Entry は**カタログ (自分のコード) の写し**なので本来は無害だが、glogx は
// snapshot から Result ごと復元する経路を持ち、そこでは Entry も保存ファイルの中身になる。
// 🚨 これは **issue 229 (復元した Entry をカタログへ束ね直す) の代わりにはならない**。
// ここが直すのは制御文字だけで、「保存された Risk / DeleteVia が実物と違う」という
// 意味のずれは残る。
func SanitizeEntryForDisplay(e Entry) Entry {
	e.Label = termsafe.PlainLine(e.Label)
	e.Recover = termsafe.PlainLine(e.Recover)
	e.Detail = termsafe.PlainLine(e.Detail)
	e.DeleteVia = termsafe.PlainLine(e.DeleteVia)
	e.Unverified = termsafe.PlainLine(e.Unverified)
	e.Risk = Risk(termsafe.PlainLine(string(e.Risk)))
	return e
}

func sanitizeLines(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, termsafe.PlainLine(s))
	}
	return out
}

// SanitizeDeleteReportForDisplay は削除の結果 (確認パネル・結果パネル・`y` のコピー) を
// 無害化する。対象パスと OS のエラー文、`cli:` で起こしたコマンドの stdout / stderr が乗る。
//
// 🚨 ここでは Path を**書き換える** (落とさない): この Report はもう「何をしたか」の記録で、
// 照合には使わない。落とすと「触ったのに一覧に出ない対象」ができて記録として嘘になる。
func SanitizeDeleteReportForDisplay(rep DeleteReport) DeleteReport {
	out := rep
	out.HistoryPath = termsafe.PlainLine(rep.HistoryPath)
	out.Entries = make([]EntryOutcome, 0, len(rep.Entries))
	for _, e := range rep.Entries {
		e.Label = termsafe.PlainLine(e.Label)
		e.Reason = termsafe.PlainLine(e.Reason)
		e.Command = termsafe.PlainLine(e.Command)
		items := make([]ItemOutcome, 0, len(e.Items))
		for _, it := range e.Items {
			it.Path = termsafe.PlainLine(it.Path)
			it.Reason = termsafe.PlainLine(it.Reason)
			it.Dest = termsafe.PlainLine(it.Dest)
			it.Ref = termsafe.PlainLine(it.Ref)
			items = append(items, it)
		}
		e.Items = items
		cmds := make([]CommandRecord, 0, len(e.Commands))
		for _, c := range e.Commands {
			cmds = append(cmds, SanitizeCommandRecordForDisplay(c))
		}
		e.Commands = cmds
		out.Entries = append(out.Entries, e)
	}
	return out
}

// SanitizeCommandRecordForDisplay は 1 コマンドの記録を無害化する (進捗として 1 件ずつ
// 届く経路があるので、Report と別に公開する)。
//
// 🚨 stdout / stderr は**複数行が正常**なので改行だけ残す。行に割って出す側 (glogx の
// commandLogLines) が 1 行ずつ扱う。
func SanitizeCommandRecordForDisplay(c CommandRecord) CommandRecord {
	c.Name = termsafe.PlainLine(c.Name)
	c.Args = sanitizeLines(c.Args)
	c.Stdout = termsafe.PlainBlock(c.Stdout)
	c.Stderr = termsafe.PlainBlock(c.Stderr)
	c.Err = termsafe.PlainLine(c.Err)
	return c
}
