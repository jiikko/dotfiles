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

## 後日追記 (2026-08-21): 修正後に残った穴 2 件

`7c064e6` (免除判定を文字列の窓からトークン走査 `scan_segment` + `unquote` へ作り替え) に対して
**別セッションが敵対的に攻めた**結果。上表の 4 ケースと、追加で作った 2 形
(「kill が先にあり窓が手前で切れる形」「`resize-pane -L` が免除に化ける形」) はすべて deny で
塞がっていた。以下は `7c064e6` でも再現する残穴。

ハーネス: `./tmp/hookattack/run.sh _claude/hooks/deny-bare-tmux-kill.sh`
(ケースは同ディレクトリの `cases.jsonl`。危険トークンを分割して組んでいるので、ハーネスを回す
Bash 呼び出し自体が偽陽性 deny されない)

### A. 値を取るグローバルオプションが値を消費せず、正当なソケット隔離を deny する (medium)

`scan_segment` が値を消費するのは `-L` / `-S` だけだが、tmux のグローバルオプションで値を取るのは
`-c` / `-f` / `-L` / `-S` / `-T` の 5 つ。`-f` の値が「サブコマンド」と誤認されて
グローバル区間が閉じ、**後続の `-L` が免除に数えられない**。

| 入力 | 期待 | 実際 |
|---|---|---|
| `tmux -f /dev/null -L probe <kill>` | allow | **deny** |
| `tmux -c /bin/sh -L probe <kill>` | allow | **deny** |
| `tmux -T RGB -L probe <kill>` | allow | **deny** |

連結形 (`-f/dev/null`) と値なしフラグ (`-u` / `-2` / `-vv`) は正しく allow。`-L` を先に書けば通る。

**これは「ルールが推奨する隔離の形を hook が拒否する」向きの誤り**で、回避のために hook を
無効化したくなる圧力を生む (deny 側の誤りより有害な向き)。最小修正は値を取る 5 オプションで
`skip_val` を立て、免除に数えるのは `-L` / `-S` のときだけにする。

### B. `\tmux <kill>` が素通りする (low)

トークンが `\tmux` になり `case tmux | */tmux` に一致しない (フルパス版 `\/opt/.../tmux` は
`*/tmux` に当たるので deny される)。修正は照合時に先頭のバックスラッシュを剥がすだけ
(`case "${t#\\}" in`)。

### 穴でないと判断したもの (記録)

- `tmux -L <kill トークン> kill-session -t x` の allow は**正しい**。ソケット名がその文字列という
  だけで、ソケットは明示されている。攻める側の最初の期待値 (deny) が誤りだった
- `tm''ux <kill>` の素通りは意図的な難読化なので hook の対象外と判断した。塞ぐより
  「hook は事故を止める装置で、故意の回避は止めない」と明記する方が正しい
  (`tmux-probe-requires-socket-isolation.md` の「hook が強制するのは上記パターンだけ」と同じ立場)

