# window list の「放置フェード」— 最近作業した window を点灯させる

status バーの window list で、**最近 shell でコマンドを実行した window ほど派手に光る**機構。
目的は「最近触った window の発見性」。十数 window を並走させていると「さっきまで作業して
いた window はどれだっけ」を毎回目視で探すことになるため、作業した場所そのものを
バー上で発光させる。2026-07-04 実装。

## 見た目（`@fade-step-secs` 秒ごとに 1 段暗くする）

非 current window のセルに適用される（current は Coral の島 `@cur-accent`）。最後のコマンド実行
からの経過を `@fade-step-secs`（現 5 秒）刻みの「段（bucket）」に落とし、明るい紫から 5 段かけて
地に溶かす（黄昏の残光: 橙の陽=現在地が沈んだ場所に紫の残光が残り、闇へ冷めていく。
旧シアン 51→23 はオレンジ基調テーマで「基調から浮く」ためバイオレットへ変更 2026-07-16。
経緯: issues/done/017-feat-claude-code-orange-theme-2026-07-16.md）:

| 段 (bucket) | 条件（step=5 秒時） | 表示 (bg × fg) |
|---|---|---|
| 実行中 | 前面でプロセスが動いている pane を持つ (@busy) | **最明 colour201 × 黒。終わるまで常時** |
| 0 | 〜5 秒 | colour201（明るい紫）× 黒 |
| 1 | 5〜10 秒 | colour164 × 黒 |
| 2 | 10〜15 秒 | colour127 × 明灰 colour252（紫は緑成分ゼロで輝度が低く、黒字はここから読めない） |
| 3 | 15〜20 秒 | colour90 × 明灰 |
| 4 | 20〜25 秒 | colour53（闇へ）× 明灰 |
| 5（消灯）| 25 秒以上 or 実行履歴なし | 背景なし（バー地に溶ける）× colour240 |

> step=5 秒なので 25 秒（5 段）かけて消灯する。刻みを変えるなら定数 `@fade-step-secs` だけを触る
> （表の秒数はこの現行値）。旧 30/60 秒の 3 段階閾値（`@fade-hot-secs`/`@fade-warm-secs`）は
> 2026-07-14 に廃止し、毎 step 秒 1 段の連続減衰へ移行した。

- bg のバイオレット階調は 256色 cube の対角 (r=b) `colour(16 + 37×(max−bucket))`（201→164→127→90→53）を算術生成する。
  この式は `@fade-ramp-color` に 1 度だけ定義し、`@fade`/`@fadetrifg`/`@fadetribg` が参照する（色を変える
  なら `@fade-ramp-color` の 1 箇所）。最明色（busy/bucket0）は `@fade-hot-bg`、段数上限は `@fade-bucket-max`、
  文字色 3 定数（`@fade-hot-fg`=黒 / `@fade-dim-fg`=明灰 / `@fade-cold-fg`=消灯）と、すべて `@fade-*` が出典
- truecolor の連続グラデにしないのは、tmux の format 算術に 16 進整形が無く `#RRGGBB` を組めない
  ため（実測確認）。6 階調の cube で近似する。grayscale 24 段の方が滑らかだが「上品だが目に飛び
  込んでこない」ため不採用（2026-07-04。発見装置なので奇抜な色に振る）
- 「実行履歴なし（未スタンプ）」を消灯に統合しているのは意図的。「光っている = 最近作業した」の
  一義性を優先（第 4 の中間色を置くと明暗差が濁る）
- bell のシアン反転（旧オレンジ 208。シアンは fade から通知役へ転用 2026-07-16）・zoom の暗赤背景は従来どおりフェードより優先される

## 「アクティブ」の定義（最重要の仕様）

**その window の shell がコマンドを実行した時刻**だけが起点。具体的には zsh の
preexec（実行開始）と precmd（実行完了）でスタンプされる。

- ⚠️ **window を select して前面に出しただけでは絶対に若返らない**（ユーザー要件
  2026-07-04）。「見た」と「作業した」は別物。当初 after-select-window hook で実装したが
  この理由で廃棄した。select 契機の再導入提案は棄却してよい
- Enter 空打ち・シェル起動直後の初回プロンプトもスタンプしない（precmd は preexec が
  直前に走った時だけ書く。_TMUX_TOUCH_PENDING フラグ）
- tail や Claude が**出力し続けているだけ**でも若返らない（#{window_activity} を使わず
  自前スタンプにしている理由。activity は出力で更新されてしまう）
- 長時間コマンド（claude / make 等）の**実行中は @busy 判定で常時点灯**する
  (window 内のどれかの pane の #{pane_current_command} が zsh 以外なら点灯固定。
  2026-07-04 追加)。tail -f / ssh 等の常駐 pane を持つ window が常時点灯になるのは
  仕様（「見張り続けている作業」扱い）。終了して shell に戻ると経過時間評価に復帰する

## 仕組み（2 つの部品）

```
[書き込み側] zshlib/_tmux_window_name.zsh の _tmux_stamp_window_touched
  zsh preexec/precmd → tmux set-option -w -t $TMUX_PANE @last-touched <epoch>
      ↓ (window user option)
[表示側] _tmux.conf の @fade-bucket / @fade-ramp-color → @fade / @fadefg
  window-status-format が #{E:@fade} で epoch と現在時刻の差を段 (bucket) に落とし分岐
```

- 表示側の現在時刻は `@epochfmt='%s'` を `#{T:@epochfmt}` で strftime 展開する
  トリック（status-left の @secfmt と同型）。追従はステータス再描画単位
  （status-interval=1、prefix 点滅の駆動と共用）なのでほぼ即時。window 切替等の操作でも再描画
- `@fade` は bg+fg（セル先頭用）、`@fadefg` は fg のみ（pane 数・claude アイコン後の
  文字色リセット用）。分かれている理由は zoom の暗赤背景を途中で潰さないため。段計算は
  `@fade-bucket`、bg 色式は `@fade-ramp-color`、最明は `@fade-hot-bg`、段数上限は `@fade-bucket-max` に
  集約したので、@fade / @fadefg / @fadetrifg / @fadetribg の 4 変数が自動で同期する（定数を 1 箇所
  変えれば全変数へ伝播。以前は色式を 3 変数に複製していたが 2026-07-15 にヘルパーへ集約した）
- スタンプの throttle は 3 秒（プロンプト毎に tmux client を fork しない方針。連続作業中でも
  3 秒に 1 回の fork に上限が付くので CPU は軽微）。⚠️ throttle は `@fade-step-secs`（5 秒）以下で
  なければならない。throttle > step だと @last-touched が最大 throttle 秒古いまま残り、離れた直後の
  window が数段沈んで見え、発見性という主目的が壊れる（2026-07-14 に 5 秒フェード化と同時に 30→3 へ）

## よくある「効いていない」の正体

| 症状 | 原因と対処 |
|---|---|
| 導入・変更直後に何も変わらない | 全 window が未スタンプ = 消灯スタートは正常。作業した window から点灯していく |
| `ls` を打っても点灯しない | その zsh が**古い**（lib 読み込みはシェル起動時のみ。`tmux source-file` では zsh は変わらない）。`exec zsh` か `source ~/dotfiles/zshlib/_tmux_window_name.zsh` |
| スタンプはあるのに色が変わらない | 再描画待ち（status-interval=1 なので通常 1 秒以内）。window 切替で即反映 |
| resurrect リストア後に全部消灯 | 正常。@last-touched は resurrect に保存されず、復元 pane は新しい zsh を起動するので以後は自動で効く。**リストア後の手動作業は不要** |

デバッグの入口: `tmux list-windows -a -F '#{session_name}:#I #{@last-touched}'` で
スタンプの有無を見る。手動で点灯テストするなら
`tmux set-option -w -t :2 @last-touched $(date +%s)`。

## ハマった実装上の落とし穴（再発防止）

- **$EPOCHSECONDS は zsh/datetime モジュール必須**。未ロードだと空になり、throttle の
  算術が常に真 → スタンプが silent に一切走らない。lib 内で `zmodload -i zsh/datetime`
  を自己保証している（zshrc 側のロードに依存しない）
- **`#{session_id}` を run-shell の引数に使ってはいけない**（過去の select 契機実装で
  発覚）。session_id は `$0` 形式で、run-shell が展開後の文字列を `sh -c` に渡す際に
  シェルの positional parameter として食われて消える
- 一括移行時（既存 shell への注入）は `pane_current_command == "zsh"` の pane に限定して
  send-keys すること。claude / nvim / ssh 等が前面の pane に文字列を流すと事故る

## 関連

- 実装: `_tmux.conf`（`@fade` / `@fadefg` / window-status-format）、
  `zshlib/_tmux_window_name.zsh`（`_tmux_stamp_window_touched`）
- 隣接機能: @claude_state アイコン（Claude の作業状態）/ bell 閃光システム（完了通知）/
  点火アニメ（window 切替時に current 島が暗赤→蛍光へランプ。scripts/tmux_ignite_current.sh、
  docs/theme-colors.md 参照）。フェードの「離れた場所が冷める」と点火の「入った場所が点く」は対の演出。
  フェードは「どこで作業していたか」、@claude_state/bell は「いま何が起きているか」で役割が異なる
