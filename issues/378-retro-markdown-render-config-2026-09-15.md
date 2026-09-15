# 378 retro: markdown 表示設定の見直し (2026-09-15)

対象: 「nvim の markdown が読みにくい / plain にすると高さがガバッとずれる」の相談から、
render-markdown.nvim の設定を案 B へ寄せる commit (cde29e5e) まで。

## 入れたもの

| commit | 内容 |
|---|---|
| cde29e5e | `render_modes = true` / 見出しの帯を廃止 / `code.border = "thin"` / `pipe_table` の上下枠線を廃止。`RenderMarkdownH{i}Bg` の上書きループを削除 |

実測: `docs/README.md` (テーブル 6 個) を幅 120 で開くと 119 行 → 枠線なしで 98 行。
`render_modes = true` では normal と insert の画面が capture-pane 上で完全一致。

## 気づき

### 1. 見本ハーネスが 4 通りの壊れ方をし、どれも「もっともらしい数字」を出した 🔴

このセッションで作った測定は、全部で 4 回、**動いていないのに結果を返した**。

| 壊れ方 | 出た嘘 | 気づいた手段 |
|---|---|---|
| headless で `RenderMarkdown disable` が効かない (marks も conceallevel も変わらない) | 「toggle しても高さは変わらない」 | 出力に `conceallevel=3` と `marks=33` が両方の行に並んでいた |
| headless の `nvim_feedkeys("i")` で insert に入らない | 4 案すべて「ずれ +0 行」 | `cur.lua` は定義上 insert で marks=0 になるはずなのに 102 のままだった |
| BSD の `diff` に `--unchanged-group-format` が無い | 「画面 44 行中 44 行が変化」 | usage が stderr に出ていたが、stdout の数字だけ読めば通っていた |
| `nvim -u <別パスの init>` が描画を一切しない | 「案 B を当てると装飾が全部消える」 | **変更前の版で同じ手順を踏んだ** (canary) |

4 つ目が最も危なかった。適用直後のキャプチャが素の markdown だったので、
**自分の変更が壊したように見えた**。`verify-execution-not-just-exit-code.md` の
「隔離環境での *失敗* も本番の失敗ではない」がそのまま当たっており、
そこに書かれている「nvim -l はユーザー設定を読まない」の隣の形 (`-u` は読むが描画しない)。

→ 切り出し先: [`instrument-before-second-fix.md`](../_claude/rules/instrument-before-second-fix.md)
への**追記**。issue 368 の項目 3 が同じ節へ足そうとしている
「nvim なら `--server <socket> --remote-expr` で生きているセッションを直接読める」の隣に、
**「headless / `-u` の nvim は設定を読むが、描画・モード遷移・win_options の復元は起きない。
見た目とモードが絡む観測は隔離 tmux の実端末で撮る」**を足す。368 の項目 3 と同じ場所なので、
**切り出すなら 1 回でまとめる**。

### 2. 効果量を測る前に主犯を決めかけた 🔴

`code.border = "hide"` (nvim 0.11 の既定が ``` の行を `conceal_lines` で消す) を見つけた時点で
これを主犯に据えかけた。実際に幅 219 で測ると**最大 3 行**で、「ガバッと」には届かない。
advisor の指摘で「主訴はまだ一度も再現していない」と認め、ユーザーに聞いたら
**insert モードに入った瞬間**で、toggle ですらなかった。

機構を見つけたことと、それが主訴の原因であることは別。
→ 切り出し先: **なし**。[`instrument-before-second-fix.md`](../_claude/rules/instrument-before-second-fix.md)
の「『何度直しても直らない』と言われたら、まず非対称を特定する」が既に同じことを言っている
(今回は「どの操作で起きるか」を聞く前に機構の説明を始めた)。実例として rationale 側へ。

### 3. 見本は効いたが、見本と採用後の見た目は一致しなかった 🟡

3 案を実物 (`README.md` / `docs/README.md`) で出して 1 往復で決まった
(memory の「色/デザイン提案は視覚見本を添えると即決される」どおり)。
ただし見本は `_nviminit.lua` の hl ループに触っていないため、
**採用後は見出しの文字色だけ残る**という差があり、提示で断り書きが要った。

→ 切り出し先: **却下 (既存で足りる)**。
[`decide-layout-in-sample-renderer-first.md`](../_claude/rules/decide-layout-in-sample-renderer-first.md)
に issue 368 項目 1 の「見本は実物の分布で描く」を足す話が既にあり、
今回の「見本は本体の一部 (hl 設定) を再現していない」も同じ節の射程。
368 の切り出しにこの形を 1 行足せば足りる。

## 残課題

- [x] 項目 1 を `instrument-before-second-fix.md` へ追記した (issue 368 の項目 3 と同じ節へ
      **まとめて 1 回で**。「動いているプロセスに問い合わせ口があるなら直接聞く」と
      「headless / `-u` は設定を読むが描画・モード遷移が起きないので見た目の観測に使わない」の 2 項)
- [x] 項目 2 は切り出さない (既存ルールで足りる)。実例は
      `_claude/rules-rationale/instrument-before-second-fix.md` へ移した
- [x] 項目 3 は却下 (368 の切り出しに 1 行足せば足りる)。その 1 行
      (「見本が本体設定の一部を再現していないなら提示時に差を断る」) は
      `decide-layout-in-sample-renderer-first.md` へ入れた
- [x] `tmp/md-sample/` を捨てた (2026-09-15。結論は本 issue と cde29e5e のコメントに移済み)

## 切り出しの結果 (2026-09-15)

| 追記先 (規範) | 内容 | 出典 |
|---|---|---|
| `_claude/rules/instrument-before-second-fix.md` | 生きているプロセスへ直接問い合わせる / headless・`-u` は本番の見た目を再現しない | 378 項目 1 + 368 項目 3 |
| `_claude/rules/decide-layout-in-sample-renderer-first.md` | 見本は実物の分布で描く / 見本が再現していない層は断る | 368 項目 1 + 378 項目 3 |
| `_claude/rules/verify-execution-not-just-exit-code.md` | 派生を持つ検証スクリプトは派生どうしが違う値を出すことを確かめる | 368 項目 2 |

実例・実測・起源は同名の `_claude/rules-rationale/*.md` へ置いた (CLAUDE.md の
「本文には規範だけを書き、実例は rationale へ最初から書く」)。368 の残課題も同じ変更で空になった。
