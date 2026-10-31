# 既存の呼び出しに「新しい値・フラグ・経路」を通す修正では、受け側のガードを先に洗う — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/survey-receiver-guards-before-passing-new-values.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: obaket issue 514 / 518, 2026-08-21)

同一セッションで**同じ構造の見落としを 2 回**やり、どちらも敵対的レビューが P1 として摘出した:

1. **issue 514**: 削除呼び出しに `isDirectory: true` を渡す修正を入れた結果、受け側
   (`S3Adapter.deleteImpl`) の**非空 preflight** (issue 309 で追加されたガード) を
   その経路が初めて踏むようになった。呼び出し側 (reconcile executor) の catch は
   412 系しか想定しておらず、reconcile 全体が中断 → **修正前より悪い永久 wedge** になった
2. **issue 518**: directory 行を既存の削除安全判定 (`sourceDeletionSafety`) に流した結果、
   file 前提の「snapshot 必須 = 無ければ unverifiable」が directory (S3 では snapshot が
   構造的に nil) に誤適用され、**本命ケースが恒久 attention** になった

共通点: 修正自体は正しい方向で、単体では green だった。**壊れたのは「新しい値で初めて
到達する既存ガードとの相互作用」**で、fake がガードを模していなかったためテストも素通しした。

## ルール本文から移した実例

本文には規範だけを残し、その根拠になった実例をここへ移した（元の文脈のまま）。

 (実例 2026-08-21 obaket 536:
  page ループを共通 helper に置換したら、helper の新 throw (`serviceUnavailable`) を受ける
  変換 (部分削除の件数を載せる責務) が空白になり、同日に自分で確立した不変条件
  「部分削除は明示報告」を自分の refactor で壊しかけた)

## 「自分が宣言した不変条件」節の起源 (dotfiles, 2026-09-06)

glogx の監査対応 21 件を実装したセッションの敵対的レビューで、**同じ形の欠陥が 4 件**出た。
どれも「対にして持つと決めたのに、対を崩す経路を洗わずに片方だけ更新する箇所を残した」:

| 対にしたもの | 崩した経路 | 症状 |
|---|---|---|
| `detailsLoading` / `detailsWaiting` (包含関係をコメントで宣言) | `detailMsg` / `basisMsg` が loading しか消さない | 実取得中の札が世代交代で落ち「取得中なのに CI job 情報なし」 |
| `rows` / `rowsGen` (世代カウンタ) | AST ゲートが引数経由・var 宣言経由の代入を見ない | 世代が進まず displayRows が恒久 stale |
| `diskHasDetail` (述語) / `diskDetail` (builder) | 述語を作らず `hasDetail: true` 固定 | 展開しても 0 行の行で q が飲まれ doctor が閉じない |
| 見出し / 同一性 | 見出しの不一致を「別 issue」と断定 | 移動 + 改題で本文が閉じ、起きていない事象を告げる |

修正はいずれも「更新の責務を 1 関数へ寄せる」形にした
(`clearDetailsFlags` / `collapseTargetAtCursor` / `diskHasDetail`)。
`clearDetailsFlags` については迂回を AST ゲートで止めている
(`src/glogx/details_flags_test.go`: 走査 75 件 / 札の delete 2 件 / 違反 0 件)。

なお、`collapsibleAtCursor` と `collapseAtCursor` が同じ判定の第 2 実装を持っていたのは
**既存コード側**の同型で、修正中に「片方を直したのに挙動が変わらない」ことで見つかった。

## 既にある欄を使う前に受け側で意味を確かめる (retro 474)

- 2026-09-25: pro-con の PM が、PG への指示を `card add --request` (人間の原文の欄) に書き続けた。受け側の `dispatcher.Prompt` を読むと `--prompt` が最初から PG への指示の欄で、画面の見出し「PM に渡した指示」も中身と食い違っていた
- 追記の要否は codex (gpt-6-astra) に判定させ、このルールの発動点が「修正を書こうとした瞬間」で、コードを変えずに既存の欄を選ぶ場面が外れるとして採った

## 本文から移した実例 (2026-09-28 の prompt-audit。本文は規範だけにするため)

- 実測 2026-09-04 dotfiles 228: doctor の `Item.Path` を無害化で書き換えると `planDelete` の照合から外れ「見えているものと消えるものが違う」を作った
- 実測 2026-09-24 pro-con: backend に絞り込みを入れても、 画面が自分で読む経路から外の情報が出続けた
- obaket retro 982: 上げる pkg を固定のパスにしたのに、その元の archive は `$(PROJECT_NAME)` の場所に作っていて、 `PROJECT_NAME=Foo` で検査していない archive の pkg が上がった。修正前は失敗していた手順が通るようになった
- 実測 2026-09-06: dotfiles で 1 セッションに 3 組

## 正規化との一致で判定した (retro 755, 2026-10-14)

glogx の issues viewer で、クリップボードへ入れてよいかを `termsafe.PlainLineKeepTabs(s) == s` で判定した。無害化は
制御文字のほかに絵文字の VS16 も落とすので、⚠️ を含む名前まで「制御文字を含む」として断っていた。敵対レビュー (壊す) が
見つけ、`termsafe.IsPlain` (含むかの述語) にタブだけ許す形へ直した。

## 「状態 × 読み手」の表 (2026-10-14、vlc-multi-video-player retro 417)

タブ切替中に次の切替が来たら進行中の復元を追い越す変更 (vlc-multi-video-player issue 399) で、codex の敵対的レビューを 7 周回した。
指摘を直すたびに、次の周で直前の修正が作った退行が出た:

- 1 周目の修正「grid は常に live を capture する」→ 3 周目の P1「復元が始まる前に追い越したタブの動画が消える」
- 3 周目の修正「未開始なら snapshot を正とする」→ 4 周目「その間に届いたダウンロード完了 / 速度・ミュートが消える」
- 4 周目の修正「restore 中は restore 元の速度を正とする」→ 5 周目「途中でユーザーが変えた速度が消える」

原因は、「snapshot と live のどちらが正か」という規則を、指摘された経路ごとに直していたこと。状態 (未開始 / 途中 / 完了 / 取り消し) と
読み手 (suspend / export / cloud の完了通知 / capture) の表を最初に書いていれば、各周の退行はどれも表のセルとして事前に見えた。

同じ retro で提案した「ユーザーが変えたかを値の比較で判定しない (A → B → A を見落とす)」は、表の「途中 × ユーザー操作」のセルで
同じ穴が見つかるので却下した。

## 値から別の事実を推定する読み手 (2026-10-15、obaket retro 1138)

obaket issue 591 で、upload の single の入口も送信中の byte 進捗を出すようにした。SFTP の provider contract のテストは、
upload の入口 (single / streaming) を transfer の progress の列から推定していた (列が `[]` なら single)。single も進捗を出すようになったので、
テストは入口を streaming と取り違えて落ちた。値そのもの (進捗の列) を assert するテストではないので、進捗の変更の差分からは見えず、
切り分けに一時 worktree で 1 hunk ずつ当てる手間がかかった。

## 順序で合流させる規則と、書き手の記録 (2026-10-19、retro 833)

statusline が書く利用枠のキャッシュ (`claude-rate-limits.json`) は、複数のセッションの値を「同じ窓の使用率は下がらない」として
高い方を残していた。出所の分からない 7d=99% が一度入ると、サーバも Claude Code も 38% を返しているのに、リセットまで戻らなかった。
observedAt は値の書き換えと別の経路 (transcript の更新) で進み続け、古い値を予備へ落とす 15 分の安全網も働かなかった。
直し方は「書いた後にやり取りがあったセッションの値は上下どちらへも採る」という権威の経路を足すことだった。
99 を書いたセッションは、キャッシュに書き手の記録が無かったので特定できず、記録は修正の後に足した。

## 評価の時点 (retro 1003, 2026-10-30)

Go のテストの待ちを `testwait.Recv` に機械的に寄せたとき、`case <-time.After(d): t.Fatalf("…%s", r.Text())` の文を待ちの引数へ移し、
出力の読み取りが時間切れのときから待ち始めへ移った (待つ前の出力を出し、別の goroutine が書く変数を読んで競合しうる)。書き換えの見本を
目で見て気づき、`testwait.Diag` の関数の中で作る形に直した。

## 本文から移した実例 (2026-10-30。常時読み込みの字数を減らすため、rule の本文の括弧書きから移した)

- retro 980: 「起点が repo 全体か」で allow-group の効き方を分け、CI の gate では正しい印まで効かなくなる設計を敵対的レビューで取り下げた
- retro 786: prefix の問い合わせに上限を付けて "" を返したが、直後の同じ tmux への display-popup が止まったままだった
- obaket retro 1293: 「production の挙動を変えない」段で shutdown に後片付けの join を足し、接続の保存が背景の close の最大 60 秒を serializer を持ったまま待った
- vlc-multi-video-player retro 426: SAC の `ToastManaging` に足したホバーの停止が、obaket の logging decorator を通ると効かなかった。ビルドも警告も通り、敵対レビューで見つかった
- retro 968: キーにだけ「実行中なら断らない」枝があり、「挙動を変えない」と書いた設計が反証レビューで崩れた
- retro 819: 丸めてから割る関数に寄せた後、テストが 2 つの関数を手で繋いだままで、寄せた関数を壊す変異が緑だった
- retro 831: 「読めなかった回は前の値を残す」で、同じ値を読む初期設定の計画が古い網で sudo の板を出し直した
- obaket retro 1171: 出発地の読み直しで敵対的レビューが 7 周した
- retro 968: 面か前段の断りを 1 つずつ数え漏らし、敵対的レビューが 3 周した
- retro 833: 「同じ窓では高い方を残す」で 利用枠のキャッシュが 7d=99% に張り付き、正しい 38% が来てもリセットまで戻らない状態だった
- retro 833: 99 を書いたセッションを特定できなかった
- obaket retro 1347: URLProtocol の stub が 206 を Content-Type なしで返し、URLSession が中身の推し量りのため本文が届くまで応答を渡さなかった。「header の後で本文が止まる」状態を作るつもりの取り消しのテストが header の段で止まり、取り消しの退行を当てた変異が緑だった
