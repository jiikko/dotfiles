# 592 (bug): termwidth.StripSGR / DropColumns が OSC などを SGR として扱い、断片と BEL を残す

起票日: 2026-10-01

## 概要

`termwidth.StripSGR` と `DropColumns` は ESC の後ろを「最初の英字で終わる」(`isSGRTerminator`) と読む。SGR (`ESC [ … m`)
にはそれで足りるが、OSC (`ESC ] … BEL` / `ESC ] … ESC \`) など英字で終わらない列は**途中で切られ、残りが本文として残る**。

tuikit の README は、markdown は本文の制御文字を termsafe で落とし、highlight は「入力の無害化は使う側 (termsafe)」と書く (toast もコードでは入口で `termsafe.PlainLine` を通す)。
layout と confirm が無害化するかは書いておらず、`Panel` の `colored=false` で効くのは「中身の SGR も落とす」(README の
不変条件の節) だけ。なので問題は 2 段に分かれる:

1. tuikit: 「SGR を落とす」関数が SGR 以外の ESC 列を**半端に**削り、BEL や中身の断片を残す (落とさないより悪い形)
2. 消費者: 無害化していない外部の文字列が `confirm.Dialog` / `layout.Panel` に届く経路は、今のところ見つかっていない (下記)。
   なので実害は「将来、無害化を忘れた呼び出しが足されたとき、2 段目の守りが半端に効く」形に留まる

## 詳細

### 実測 (2026-10-01、`go run` の probe)

- `StripSGR("a\x1b]0;PWN\x07b")` → `"aWN\ab"` (`ESC ]0;P` だけ落ち、`WN` と BEL が残る)

監査の調査役の測定 (main では再測していない):

- ST 終端の OSC 52: `StripSGR("ab\x1b]52;c;QQ==\x1b\\cd")` → `"ab;QQ==d"`
- `Panel` の title `"t\x1b]0;PWN\x07x"` → `"tWN\ax"`
- `DropColumns("a\x1b]0;PWN\x07b", 1)` → OSC を SGR として溜め、結果の先頭で replay する (OSC がそのまま出る)

### 未無害化の入力が届く経路 — 見つかっていない

起票時は「glogx の job 名が `dropEmojiVS16` しか通らない」と書いたが、**誤り** (反証レビューで指摘され、main で確認)。
`src/glogx/github.go` の check の詳細を組むところで、job 名は `CheckDetail{Name: sanitizeDetailLine(name), …}` を通り、
`termsafe.DetailLine` で OSC / DCS ごと落ちる。`tui.go` の `askRerun` が確認ダイアログへ渡す `job.Name` はこの `CheckDetail` のもの。
pro-con の回答フォームの質問文・選択肢も `src/pro-con/card/choices.go` の `clean` (`termsafe.PlainLine`) を通ってから届く。

なので、無害化していない外部の文字列が `layout.Panel` / `confirm.Dialog` / `DropColumns` に届く経路は、今のところ**確認できていない**。
この issue は「tuikit の関数が、SGR 以外の ESC 列を半端に削る」という tuikit 内の頑健性の問題として扱う。
`confirm.Dialog` / `confirm.Box` / `layout.Panel` の production 呼び出しは調査役の数えで 11 か所。全部の入力の由来は追っていない。

## 対応方針

- tuikit: layout / confirm が無害化しない (使う側の責任) ことを README に明記する。そのうえで `StripSGR` / `DropColumns` の ESC の読み方を、CSI (`ESC [` … 終端バイト 0x40–0x7E)・OSC / DCS / APC 等の文字列型
  (BEL か ST で終わる)・2 文字の ESC 列に分ける。SGR 以外は replay せず落とす。x/ansi のパーサを使えるならそれに寄せ、自前の分類を書かない
  (`adversarial-review-own-safeguards.md` 0-B)
- 回帰テスト: 上の 3 つの入力 + 閉じない OSC (`ESC ]` で行末まで) を `StripSGR` / `DropColumns` / `Panel(colored=false)` に通し、
  ESC・BEL・C1 が出力に 0 件、可視の本文 (`a`・`b`) は残ることを見る
- 消費者側に直すものは今のところ無い。新しく外部の文字列を板・ダイアログへ出すときは termsafe を通す (README に書く)

## 関連ファイル

- `src/tuikit/termwidth/termwidth.go` (`StripSGR` / `DropColumns` / `isSGRTerminator`)
- `src/tuikit/layout/panel.go` (`Panel`)
- `src/glogx/github.go` (job 名は `sanitizeDetailLine` 済み) / `src/pro-con/card/choices.go` (`clean`)

## 進捗

- [ ] tuikit の ESC 列の読み方を直す + 回帰テスト (変異で red を確認)
- [ ] README に「layout / confirm は無害化しない」を書く

## 決着 (2026-10-01 の見直しで閉じる): 416 の P3-2 の重複

同じ問題が 416 (done) の P3-2 で既に記録され、「記録のみ (異常な入力か定数の誤りでしか届かない)」と判断済みだった
(`DropColumns` / `StripSGR` が「最初の英字まで」を ESC 列と読み、OSC 8 で解釈が食い違う)。起票の前にこれを見落とした。
この issue が足したのは BEL が残る・`DropColumns` が OSC を replay する、という症状の細部だけで、届く経路はこの issue も見つけていない。
pro-con の状態ファイルにも OSC (`ESC ]`) は 0 件。416 の判断を覆す根拠は無いので閉じる。

**再開の trigger** (416 と同じ): 無害化していない外部の文字列を `layout` / `confirm` / `DropColumns` に渡す呼び出しが足されたとき。
