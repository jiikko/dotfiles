# 069 bug: kill 系 deny フックの socket 免除判定が連結経路で全文評価になっており素通りする

起票日: 2026-08-20

`/audit` (品質バッチ, forge Standard) の High。**main agent が実際に素通りを再現済み**。

## 問題

`_claude/hooks/deny-bare-tmux-kill.sh` は「ソケット未指定の破壊的 tmux コマンド」を deny する
自作の安全機構。セグメント分割経路では免除 (`-L` / `-S`) の判定を
**tmux トークンと kill トークンの間に限定**し、行内コメントも落としている。

ところが `\;` (tmux クライアント内連結) を拾う先頭の判定だけが、免除を
**コマンド全文**に対して評価している (:69 付近):

```sh
if [[ "$normalized" =~ \\;[[:space:]]*$KILL_RE ]]; then
  [[ "$normalized" =~ $SOCK_RE ]] || deny "$REASON_KILL"
fi
```

結果、行のどこかに `-L` があれば連結経路の破壊的コマンドが通る。

## 実測 (2026-08-20)

フックへ直接 hook JSON を流して確認した (トークンは分割して自分の Bash 呼び出しが
偽陽性 deny されるのを避けた):

| 入力 | 期待 | 実際 |
|---|---|---|
| 素の形 (ソケット未指定) | deny | deny ✅ |
| 連結経路 (ソケット未指定) | deny | deny ✅ |
| `tmux -L probe ls; tmux new-session -d \; <kill>` | deny | **素通り** ❌ |
| 連結経路 + 行内コメントに `-L probe` | deny | **素通り** ❌ |

## 発火条件

「probe 用に `-L` を付けたコマンドと、後片付けの連結コマンドを 1 行に書く」
= まさに 2026-07-30 の 29 セッション誤殺と同じ形の 1 行。免除条件を満たしたつもりが無い
状態でフックが黙って通すため、**フックがあることを根拠に安心している場面ほど危険**。

## 推奨対応

- 連結経路も免除判定を「tmux トークンと kill トークンの間」に限定し、行内コメントを落とす
  (セグメント側と同じ前処理を共有する。判定の出典を 1 つにする)
- `tests/claude/test_deny_bare_tmux_kill.sh` に上表の 4 ケースを追加し、変異
  (免除判定を全文評価に戻す) で red になることを確認してから閉じる

## 関連

- `_claude/rules/adversarial-review-own-safeguards.md` — 自作の安全機構は自己レビューで
  閉じない。本件はまさにその実例 (フック自身のコメントが「レビューで実証されたバイパスを
  塞いだ形」と書いている箇所の隣に、同種の穴が残っていた)
- `_claude/rules/tmux-probe-requires-socket-isolation.md` — フックが強制している規範
