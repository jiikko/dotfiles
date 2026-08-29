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

## 対応 (2026-08-26)

`scripts/with_gnu_grep.sh` と `make test-gnu` を新設した。

### 決めたこと (issue 本文の「決めるべきこと」への回答)

- **対象は grep 系のみ** (`grep` / `egrep` / `fgrep`)。`sed` / `date` / `ps` / `stat` も割れるが、
  そこまで shim すると「Linux エミュレータ」の自作になり保守が本体を超える。grep だけで
  今回の 8 箇所は捕まっているので、必要になった時点で足す
- **opt-in の別ターゲット** (`make test-gnu`)。`make test` に混ぜると手元の所要時間が倍になる。
  grep のパターンや観測ログ系の assert を触ったときに回す運用にした
- **GNU grep が無い環境では失敗させる** (skip して緑を返さない)。ただしシステムの grep が
  既に GNU (Linux/CI) なら shim 不要なのでそのまま実行する。「判定不能」を緑に畳まない

### 退行を実際に捕まえることの確認

worktree で assert を `${TT_TAB}` から `'\t'` へ戻す変異 (= issue 108 の退行そのもの) を当てた:

| 条件 | 結果 |
|---|---|
| 通常の実行 (BSD grep) | **exit 0** = 手元では気づけない |
| `with_gnu_grep.sh` 経由 | **exit 1** = 退行を捕まえた |

shim 自身も「貼れた」で終わらせず、実行前に `grep --version` が GNU になっていることを
確認してから使う (貼れた ≠ 効いている)。実体は絶対パスで解決し、解決先が shim 自身の配下なら
即座に落とす (`path-shim-must-resolve-real-binary.md`)。

### 入口ドキュメント

`_claude/rules/mutation-verify-new-tests.md` の「比較に使う正規表現の方言」の項から
`make test-gnu` を指すようにした (この罠を踏む人が読む場所)。

---

## 後日 (2026-08-29): 撤去した

issue 133 で **Linux をサポート対象外**にし、CI も全 workflow を macOS runner へ移した。
GNU grep を被せて回す `make test-gnu` / `scripts/with_gnu_grep.sh` は、対象が macOS だけに
なった時点で「正しい macOS の書き方を弾く」側に回ったため撤去した。
本 issue の判断が誤っていたわけではない (当時は CI が Linux で、差は実害だった)。**前提が変わった**。
