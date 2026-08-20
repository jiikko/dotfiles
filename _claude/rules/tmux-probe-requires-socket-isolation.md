# 破壊的 tmux コマンドは、隔離を実証してから打つ（$TMUX は TMUX_TMPDIR に優先する）

> **発動点は「破壊的 tmux コマンド（kill-server / kill-session / pkill tmux）を打つ前」**。
> 計測 probe に限らず、掃除・再現実験・デモ・スクリプトの動作確認でも同じ形になる。
> tmux ペイン内では `$TMUX` があるため、素の `tmux` は常に本番サーバを向く。

## ルール

- **破壊的 tmux コマンドの前に、隔離が効いていることを実証する**: `tmux -L <name> ls` が本番セッションを**返さない**ことを見てから打つ。これが本命の規律（下記「認識がどう破れたか」のとおり、危険なのは「隔離を忘れる」ではなく「隔離したつもりが成立している」ケース）
- **成功メッセージは隔離の証拠にならない**。`new-session` が成功しても、それは本番サーバにセッションが**追加**された場合と同じ出力
- **`$TMUX` は `TMUX_TMPDIR` より優先される**。tmux ペイン内（Claude セッションは大抵ペイン内で動く）では、`-L`/`-S` を付けない限りクライアントは `$TMUX` の socket = 本番サーバに接続する（2026-07-30 実測: `d=$(mktemp -d); TMUX_TMPDIR=$d tmux ls` が本番セッション一覧を返し、`$d` に socket は作られない）。つまり **`$TMUX` が生きている限り `TMUX_TMPDIR` だけの隔離は不十分**。`unset TMUX TMUX_PANE` 済みなら `TMUX_TMPDIR` に倒す方式は成立する（tests/tmux/test_fork_scratch.sh 冒頭がその方式の正本）
- **`-f /dev/null` は新サーバを立てない**。設定ファイル指定はサーバ**起動時**にしか効かず、既存サーバへの接続では黙って無視される。「素の設定の新サーバが立つ」という読みは誤り
- **破壊的な後片付けを依頼の外から自発で足さない**。2026-07-30 の事故では計測は依頼どおりだったが、`kill-server` は依頼に無い自発の後片付けだった。共有リソース（tmux サーバ）はユーザーの領分（[`no-unauthorized-branch-switch.md`](no-unauthorized-branch-switch.md) と同ファミリー）
- **破壊的操作に `2>/dev/null` を付けない**（失敗・誤爆が観測不能になる）
- **セッション作成〜計測〜kill を 1 コマンドに詰め込まない**。破壊的操作は影響を確認できる単位で分けて打つ
- **そもそも tmux を新規に立てる必要があるか先に問う**。2026-07-30 の事故は、この問いを飛ばしたことが起点だった（測りたかったのは `bin/glogx` ラッパーと Go バイナリ直の差で、tmux を一切使わずに測れた）

## 認識がどう破れたか（起源: 2026-07-30 の本番サーバ誤殺）

別セッションの Claude が popup 計測のため以下を実行し、`$TMUX` 優先の仕様により全コマンドが本番サーバへ向かい、サーバごと落とした。

```sh
export TMUX_TMPDIR=$(mktemp -d); tmux -f /dev/null new-session -d -s probe ...
# ...計測...
tmux kill-server 2>/dev/null; rm -rf $TMUX_TMPDIR
```

「本番を触るとまずい」という認識はあった。それでも打てたのは 3 段の誤認が重なったため（当人の追記より）:

1. `TMUX_TMPDIR=$(mktemp -d)` を書いた時点で「隔離した」と**認識が完了**した（以降、隔離は前提扱いになり疑う対象から外れた）
2. `-f /dev/null` を「素の設定の新サーバが立つ」と誤解した
3. `probe session ok` の成功出力を「隔離できている証拠」として受け取った

損失: 直前の完全な保存は 7/29 20:02（29 sessions / 90 windows）。死亡時のライブ状態は記録が無く（13:28:22 に発火した save は 0 sessions で regression guard が reject）、15:54 にこの保存から復元された（セッション一覧が保存内容と一致することを実測済み）。**つまり失われたのは 7/29 20:02 以降・約 17 時間分の変化**。保存と guard が無ければ全損だった。

2026-07-07 にも tests/tmux/test_fork_scratch.sh の bare `kill-server` が本番を直撃する同型事故がある。**07-07 の教訓はテストには実装で落ちた**（`tests/tmux/*.sh` 冒頭の `unset TMUX TMUX_PANE`）が、「テストではないアドホックな 1 コマンド」の経路には落ちていなかった。それを埋めるのが下記の hook。

## 強制手段（実装済みの部分）

- **PreToolUse hook** が Bash ツールコマンド中の bare な `tmux kill-server` / `tmux kill-session` / `pkill tmux` を deny する（`-L`/`-S` でソケットを明示した形だけ通る）。実装: `_claude/hooks/deny-bare-tmux-kill.sh`（配線: `_claude/settings.json`）
- hook が強制するのは上記パターンだけ。**隔離の実証・依頼外の破壊的操作を足さない・`2>/dev/null` 禁止は hook では強制できない**ため、本 md が正本のまま残る（[`comment-no-restate-enforced.md`](comment-no-restate-enforced.md) の区分）
- hook は Bash ツールのコマンド文字列を静的検査するため、**引用符に入っていない散文**に「tmux → kill-server/kill-session」の並びや「pkill と tmux の同居」があると偽陽性 deny になる。[`no-comment-line-starting-with-shellcheck.md`](no-comment-line-starting-with-shellcheck.md) と同族の罠
  - 実測 2026-08-21（issue 069 でトークン走査へ作り替えた後）: **引用符で囲んだ文字列**（`echo "tmux の kill-server を deny"` / `cases=("tmux kill-server" ...)`）と**コメント**（行頭 `#` / 行内 ` #`）は検査対象から外れて **通る**。deny になるのは引用符に入っていない散文だけ
  - 実務で踏むのはほぼ **heredoc 本文**。`git commit -F - <<'"'"'EOF'"'"'` のコミットメッセージ本文は引用符に入らないので、説明文がそのまま検査対象になる（このルール自体の commit で踏んだ）
  - 回避の優先順: ①引用符で囲む ②言い換える（「ソケット未指定の kill-server」等、小文字 `tmux` を同じ行に置かない）③パターン自体を書く必要がある場面（テストケース・攻撃ハーネス）では**シェル変数でトークンを分割して組む**（`K="kill-ser""ver"` として `"tmux $K"` を作る）。`tests/claude/test_deny_bare_tmux_kill.sh` は①で足りているため分割していない

## やること / やらないこと

- ✓ 破壊的 tmux コマンドの前に `tmux -L <name> ls` で「本番が見えない」ことを実証する
- ✓ probe の全サブコマンドに `unset TMUX TMUX_PANE` + ユニーク `-L`（または `-S`）を付ける
- ✓ 後片付けは自分の socket を明示して打つ（`tmux -L <name> kill-server`）
- ✓ 恒久テストは tests/tmux/ の既存方式に倣う（socket 隔離 = 冒頭の `unset TMUX TMUX_PANE` + `TMUX_TMPDIR`。`lib/isolate_env.sh` は HOME/XDG の隔離**のみ**で socket は対象外）
- ✗ `TMUX_TMPDIR` のみに依存した「隔離したつもり」（`$TMUX` が生きていると素通り）
- ✗ 成功出力を隔離の証拠として扱う
- ✗ 依頼に無い破壊的後片付けの自発追加
- ✗ 破壊的操作への `2>/dev/null` / 計測 + kill のワンライナー詰め込み

## 関連

- dotfiles scripts/CLAUDE.md「サーバ状態に触るスクリプトの不変条件」— スクリプト側の同規律
- dotfiles tests/tmux/test_fork_scratch.sh 冒頭 — 2026-07-07 の同型事故の経緯と socket 隔離の正本
- [`no-unauthorized-branch-switch.md`](no-unauthorized-branch-switch.md) — 「共有リソースへの依頼外の破壊的操作」ファミリー
- [`comment-no-restate-enforced.md`](comment-no-restate-enforced.md) — hook で強制済みの部分と md に残す部分の区分
