# window list の「放置フェード」— 最近作業した window をピンクで点灯させる

status バーの window list で、**最近 shell でコマンドを実行した window ほど派手に光る**機構。
目的は「最近触った window の発見性」。十数 window を並走させていると「さっきまで作業して
いた window はどれだっけ」を毎回目視で探すことになるため、作業した場所そのものを
バー上で発光させる。2026-07-04 実装。

## 見た目（3 状態）

非 current window のセルに適用される（current は従来どおり青い島）:

| 状態 | 条件 | 表示 |
|---|---|---|
| 点灯 | 最後のコマンド実行から 30 分未満 | **ショッキングピンク (bg=colour199) × 黒文字 (fg=colour16)** |
| 減光 | 30 分〜2 時間 | 深マゼンタ (bg=colour125) × 明灰文字 (fg=colour252) |
| 消灯 | 2 時間以上 or 実行履歴なし | 背景なし（バー地に溶ける）× fg=colour240 |

- 色をここまで派手にしているのは意図的（ユーザー要望 2026-07-04）。初版のグレー階調
  （bg238/bg240）は「上品だが目に飛び込んでこない」ため不採用。発見装置なので奇抜側に振る
- 「実行履歴なし（未スタンプ）」を消灯に統合しているのも意図的。第 4 の中間色を置くと
  点灯との明暗差が濁る。「光っている = 最近作業した」の一義性を優先
- bell のオレンジ反転・zoom の暗赤背景は従来どおりフェードより優先される

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
- 長時間コマンド（claude / make 等）は開始時と完了時にスタンプされ、**実行中はフェードが
  進む**。実行中の可視化は @claude_state アイコン（⚙🔔🔕✓）の担当で、役割を分けている

## 仕組み（2 つの部品）

```
[書き込み側] zshlib/_tmux_window_name.zsh の _tmux_stamp_window_touched
  zsh preexec/precmd → tmux set-option -w -t $TMUX_PANE @last-touched <epoch>
      ↓ (window user option)
[表示側] _tmux.conf の @fade / @fadefg
  window-status-format が #{E:@fade} で epoch と現在時刻の差を 3 段階に分岐
```

- 表示側の現在時刻は `@epochfmt='%s'` を `#{T:@epochfmt}` で strftime 展開する
  トリック（status-left の @secfmt と同型）。追従はステータス再描画単位
  （status-interval=60）なので**最大 60 秒遅れる**。window 切替等の操作で即再描画
- `@fade` は bg+fg（セル先頭用）、`@fadefg` は fg のみ（pane 数・claude アイコン後の
  文字色リセット用）。分かれている理由は zoom の暗赤背景を途中で潰さないため。
  閾値・色を変えるときは**両方を同期して編集**する（_tmux.conf のコメント参照）
- スタンプの throttle は 30 秒（プロンプト毎に tmux client を fork しない方針）。
  フェード粒度が 30 分なので表示への影響はない

## よくある「効いていない」の正体

| 症状 | 原因と対処 |
|---|---|
| 導入・変更直後に何も変わらない | 全 window が未スタンプ = 消灯スタートは正常。作業した window から点灯していく |
| `ls` を打っても点灯しない | その zsh が**古い**（lib 読み込みはシェル起動時のみ。`tmux source-file` では zsh は変わらない）。`exec zsh` か `source ~/dotfiles/zshlib/_tmux_window_name.zsh` |
| スタンプはあるのに色が変わらない | 再描画待ち（最大 60 秒）。window 切替で即反映 |
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
- 隣接機能: @claude_state アイコン（Claude の作業状態）/ bell 閃光システム（完了通知）。
  フェードは「どこで作業していたか」、これらは「いま何が起きているか」で役割が異なる
