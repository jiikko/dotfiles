# テストを GNU grep 条件で回す手段がなく、platform 依存の偽緑が CI まで気づけない

起票日: 2026-08-26
種別: chore
優先度: **P2** (同じ形で 1 日に 2 回 CI を赤にした)

## 何が起きたか (2026-08-25 の実測)

`tests/tmux/test_log_restore_hook.sh` 他が **手元 (macOS) 緑・CI (Linux) 赤**になった。
原因は assert の `grep -qE '\trestore-start ...'` で、**`\t` は POSIX ERE のタブ escape ではない**。

手元で両方を叩いて確定させた:

```
$ printf 'a\tb\n' | grep -qE '\tb'     # macOS BSD grep      → マッチする
$ printf 'a\tb\n' | ggrep -qE '\tb'    # GNU grep 3.11       → stray \ before t / マッチしない
```

BSD がタブとして解釈し、GNU がリテラル `t` として扱うため、**手元では永遠に緑**になる。
同型が 8 箇所あった (うち 2 箇所は `wait_for_line` ヘルパー経由で、grep の近傍を掃く方法では
見つからなかった)。

同じ日に `ps -o lstart=` の末尾パディング (macOS だけ空白埋め) でも同じ「手元緑・CI 赤」が
起きており、**そちらは本番のバグだった** (Linux で移行ガードが機能せず生存 owner の lock を奪う)。

## 提案

`make test` を GNU 系ツールで回すためのターゲットを常設する。実効性は実証済み:

```sh
# 2026-08-25 に実際に使った手順 (path-shim-must-resolve-real-binary.md 準拠)
real="$(readlink -f "$(command -v ggrep)")"     # 実体を絶対パスで解決
case "$real" in "$shim"/*) exit 70;; esac        # 自己参照の検出
ln -sf "$real" "$shim/grep"
PATH="$shim:$PATH" ./tests/...
```

**この shim が退行を実際に捕まえることを確認済み**:

| 条件 | 結果 |
|---|---|
| 修正前のコード + GNU grep | **exit 1 (red)** = CI の失敗を再現 |
| 修正前のコード + BSD grep | exit 0 (green) = 手元の偽緑を再現 |
| 修正後のコード + GNU grep | exit 0 |

## 決めるべきこと (着手時)

- **対象コマンドを grep だけにするか**。`sed` / `date` / `ps` / `stat` も BSD/GNU で割れる。
  全部を shim すると「Linux エミュレータ」を自作することになり保守が重い。
  grep だけでも今回の 8 箇所は捕まった
- **常時実行か opt-in か**。`make test` に混ぜると手元の実行時間が倍になる。
  `make test-gnu` の別ターゲットにして「観測ログ系テストを触ったとき」の手順書に載せる案
- `ggrep` が無い環境 (CI の Linux 自身・素の macOS) での扱い。**「無いので skip」を緑で
  返さないこと** (adversarial-review-own-safeguards の「沈黙 = 成功にしない」)

## 却下した案

- **静的検査で `\t` を落とす** — peer が検討して却下済み。`\t` を含む行は repo に 35 件あり
  大半が良性 (コメント / `awk` の `print` / `paste -d'\t'` / heredoc)。正確な信号は
  「grep の ERE に届く `\t`」で、ヘルパー変数経由だとデータフロー解析が要る。
  ノイズだらけの検査は次の人が無効化するだけ
- **`grep -P` を使う** — GNU 専用で macOS が持たない。両対応にならない

## 関連

- `_claude/rules/mutation-verify-new-tests.md` — 「環境で値が変わるコマンド出力を比較に使わない」
  (今回の兄弟。あちらは測られる側、こちらは測る側 = 正規表現の方言)
- `_claude/rules/path-shim-must-resolve-real-binary.md` — shim の作法の正本
