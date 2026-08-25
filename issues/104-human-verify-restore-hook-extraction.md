# 104 human: 復元 hook 抽出後、実際に 1 回復元して観測ログを確認する

起票日: 2026-08-25
期限: 2026-09-01

## 背景

issue 079 で `_tmux.conf` の復元 hook から観測ログの書き込みを
`scripts/tmux_log_restore_hook.sh` へ抽出した (`636d916`)。**自動テストで確認できるのは
スクリプト単体の振る舞いまで**で、「tmux が実際に hook を実行して、その中でスクリプトが
起動する」ところは実際に復元を 1 周させないと分からない (テストサーバで復元を回すと
本番の観測ログを汚すため、こちらでは実行していない)。

## 確認してほしいこと

conf を reload した後、復元を 1 回走らせて (`C-t C-r` など)、`~/.cache/tt-restore-trigger.log` に
以下が**両方**出ているか:

```
<日時>	restore-start epoch=<数字>
<日時>	restore-end rc=0 epoch=<数字>
```

併せて `~/.cache/tt-restore-duration.log` に `restore=<数字>s` が 1 行増えていること。

## 出ていなかった場合に疑うところ

| 症状 | 疑い |
|---|---|
| 両方出ない | `${DOTFILES_DIR:-$HOME/dotfiles}/scripts/tmux_log_restore_hook.sh` が解決できていない (実行ビットを含む)。`_tmux.conf` の `@resurrect-hook-pre-restore-all` / `-post-restore-all` を確認 |
| `restore-start` だけ出る | post hook の途中で止まっている。**`@tt-restore-complete` が立っているかも一緒に見る** (`tmux show -gqv @tt-restore-complete`)。立っていなければ復元の途中死判定に影響する |
| duration が `restore=0s` | `@tt-restore-duration` が読めていない (pre hook が `@tt-restore-in-progress` を設定できていない) |

## 補足

`@tt-restore-complete` などの tmux オプション設定は**意図的に conf のインラインに残してある**
(スクリプトのパス解決の失敗が「復元は成功したのに途中死と記録される」に化けるため)。
なので「ログは出ないがオプションは立っている」は設計どおりの縮退で、復元自体は壊れない。
