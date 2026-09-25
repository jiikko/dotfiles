# 486 (ux): カードの詳細の「出力」を、改行を残した markdown として整形し、コードをハイライトする

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「カードの詳細画面を開いたら、出力セクションが改行されていなくて読みにくいので整形しつつ、シンタックスハイライトを有効にして」。

今の詳細 (enter の引き出し。`src/pro-con/ui/drawer.go` の `drawerBody`) の「出力」は、PG の出力の末尾 3 件を 1 件 = 1 段落で並べるだけ。
PG の出力は見出し・箇条書き・コードブロックを含む markdown なのに、全部が 1 本の長い行として折り返されて出る。

## 原因 (2026-09-26 に読んだ)

- **改行は画面に届く前に消えている**。`src/pro-con/live/transcript.go` の `text()` が、assistant の文を `oneLine`
  (`strings.Join(strings.Fields(s), " ")`) に通してから `Transcript.Outputs` に積む。詳細の側でどう折り返しても戻らない
- `Outputs` を読むのは 3 か所: `live/live.go` (カードの `Log` = 末尾 3 件。詳細の「出力」)、`cardview.go` (`pro-con card show` の出力の末尾)、
  `dispatcher/btw.go` (btw の haiku に渡す材料 8 件)
- `text()` (= `oneLine`) は `Outputs` 以外にも、人間の発言 (`Prompts`)・再開の文の判定 (`RestartNote` の前方一致)・ツールの結果の無い user 行の判定に使い、
  `signatures` (watchdog の進捗の判定。同じ文の繰り返しを進捗に数えない) も `oneLine` で正規化している。**これらは 1 行のままでよい**

## 期待する動作

- 詳細の「出力」で、PG の出力の改行・段落・箇条書き・見出しが元の形で読める (幅は引き出しの幅で折り返す)
- コードブロック (```lang) は言語に合わせて色が付く。言語の無いブロックは色を付けずに地の色だけ変える
- 1 件ごとの区切りが分かる (件の間に空行か罫線)
- 色を付けない出力 (`pro-con card show` のパイプ先・テスト) では ANSI を出さない

## 対応方針の案 (未決定の点がある)

1. `Transcript.Outputs` だけを **改行を残した原文**にする。`text()` は変えず、原文を取り出す関数を別に足して `Outputs` だけに使う
   (`text()` を変えると、上の判定と watchdog の進捗の数え方まで変わる)。1 行が要る側 (btw の材料・ほかに見つかれば) は使う側で潰す。
   🚨 受け側のガードを先に洗う: `Outputs` を「1 件 = 1 行」と前提にしている箇所 (`tail` の件数・`card show` の行数の予算・テストの fixture) を grep してから変える
2. 整形とハイライトは既存の部品を使う: glogx の `issues.RenderBody(src, width, colored)` (`src/glogx/issues/render.go`。markdown → 端末行、
   コードブロックは chroma でハイライト)。chroma は pro-con の go.mod に既に間接依存で入っている
   - **決めること**: glogx の module を pro-con から import するか、`RenderBody` (と `issues/wrap.go`) を `src/tuikit` へ移して両方から使うか。
     tuikit は pro-con と glogx が共有する UI 部品の置き場 (toast を glogx から切り出した前例がある) なので、移す方が依存の向きが素直
3. 「末尾 3 件」は件数でなく行数で決め直すか (1 件が長いと引き出しが 1 件で埋まる)。引き出しはスクロールできるので、件数のままでもよい

## 関連ファイル

- `src/pro-con/live/transcript.go` の `text` / `oneLine` / `Outputs`
- `src/pro-con/live/live.go` の `cards[i].Log = tail(t.Outputs, 3)`
- `src/pro-con/ui/drawer.go` の `drawerBody` (「出力」の節)
- `src/pro-con/cardview.go` (`card show`)、`src/pro-con/dispatcher/btw.go` の `pgOutputs`
- `src/glogx/issues/render.go` の `RenderBody` / `highlightCode`

## 関連

- 467 (作業中のカードの PG の出力と道具の呼び出しを追う。C-034 で作業中) — 同じ transcript を読む。出力の見せ方を重ねて作らないよう、467 の成果物を見てから着手する
- 469 (カードを開いたら進捗) — 同じ引き出しを触る

## 順番の見積もり (PM, 2026-09-26。C-040 を C-034 と C-035 の後に積んだ)

- 触る場所: `live/transcript.go` の `Outputs` の積み方、`live/live.go` の `Log`、`ui/drawer.go` の `drawerBody` の「出力」の節、`cardview.go`、`dispatcher/btw.go`
- 変える判断: 「`Outputs` の 1 件 = 1 行」という前提 (これを読む側すべてに効く)
- 順番の理由: 467 (C-034) は同じ transcript を読んで PG の出力と道具の呼び出しを引き出しに出す。469 (C-035) は同じ `drawerBody` に進捗の節を足す。どちらも `Outputs` の形と引き出しの並びに乗るので、その 2 つが入ってから前提を変える (依頼の原文でも指定)。473 (C-037) も `drawerBody` に節を足すが、`Outputs` は読まないので順番は付けていない
- 「決めること」(glogx の `issues.RenderBody` を import するか、`src/tuikit` へ移すか) は、PG が推測で決めずに質問する (依頼の原文)
