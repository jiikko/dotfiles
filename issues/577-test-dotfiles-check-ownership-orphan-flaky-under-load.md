# 577 (test): `test_dotfiles_check_result_ownership.sh` の「親の死後に公開しない」が並列の負荷で落ちる

起票日: 2026-09-28

## 概要

`make test` (並列 103 件) の 1 回で `tests/zshrc/test_dotfiles_check_result_ownership.sh` のケース 6 が落ちた:

```
✗ 親の死後に 1 件公開した: /var/folders/.../cache/dotfiles/setup-check.13264.result.partial
```

同じ checkout で単独 (`zsh tests/zshrc/test_dotfiles_check_result_ownership.sh`) に 3 回回すと 3 回とも通り、直後の `make test` 2 回も通った
(2026-09-28、issue 576 の作業中。576 は zsh を触っていない)。

## 詳細

- 残っていたのは公開された `.result` ではなく **`.result.partial`**。bg (`_zshrc` の `_dotfiles_check_bg`) は `.partial` を書いてから、
  親が生きていれば `mv`、死んでいれば `rm` する
- テストのケース 6 は、gate を置いて bg を解放した直後に「`.partial` が在る間は待つ」ループ (最長 `sleep 0.05` × 200) を回し、
  そのあと `sleep 0.3` してから、`.result` と `.result.partial` のどちらかが在れば「親の死後に公開した」とする
- 🚨 **待ちのループは 1 回も回らない**: gate を置いた直後は、bg がまだ shim (`shasum`) の中の `sleep 0.05` の待ちに居て `.partial` を書いていない。
  最初の判定で `.partial` が無いので即座に抜け、実際に bg を待っているのは固定の `sleep 0.3` だけ (反証レビューの指摘。テストのケース 6 のコードで確かめた)
- **仮説 (未確認)**: 負荷の下で、bg が 0.3 秒のうちに `.partial` を書いてから `rm` するところまで届かなかった。書いた後・消す前に assert が走り、
  「bg がまだ途中」を「親の死後に公開した」と読んだ。実装の退行ではなく、ハーネスの待ち方の問題
  - 反対の可能性: bg が `kill -0` で死んだ親を「生きている」と読んだ (pid の再利用)。その場合は `.partial` ではなく `.result` が残るはずなので、
    今回の出力とは合わない

## 対応方針

- 待つ対象を「bg のプロセスが終わった」にする (`avoid-wall-clock-assertions.md`: 待ちたい事象そのものを待つ)。今の「`.partial` が在る間は待つ」は
  `.partial` がまだできていない時点で抜けるので、待ちになっていない。bg の pid が取れないなら、「`.partial` ができてから消える」の両方を順に待ち、
  **時間切れはハーネスの失敗として別に出す** (公開した、と読まない)
- 固定の `sleep 0.3` は、bg の終了を待てれば要らなくなる
- 直したら、負荷の下 (例: 並列で CPU を埋める) で繰り返して落ちないことと、`_dotfiles_check_bg` の `kill -0` の分岐を外す変異で red になることを確かめる

## 関連ファイル

- `tests/zshrc/test_dotfiles_check_result_ownership.sh` のケース 6 (「親シェルが先に終わっていたら bg は公開しない」)
- `_zshrc` の `_dotfiles_check_bg`

## 進捗

- 2026-09-28 起票。まだ着手していない
- 2026-09-28 反証レビュー (sonnet 1 本): 結論の向きは反証できず。採用: 待ちのループが 1 回も回らない (上の「詳細」を直した)。
  反証できなかった: 反対の可能性 (pid の再利用なら `.result` が残る) の棄却 / 他に `.partial` が残る経路 (print の失敗は `.partial` を作らない、
  zshexit の後始末は親の終了時点で `.partial` が無いので何もしない、`reset_cache` の直後なので他ケースの残骸は混ざらない) / 既存の同じ issue (done/300 は別件)
- 2026-09-29 対応 (commit「test(zshrc): 親の死後に公開しないテストを bg の終了で待つ (issue 577)」):
  - 待つ対象を「bg のプロセスが終わった」にした。親シェルに `BGPID=$!` を出させ (`&!` でも `$!` は取れる。実測)、
    `ps -p <pid> -o stat=` が空かゾンビになるまで待つ (kill -0 はゾンビにも成功するので使わない)。上限 20 秒で、
    時間切れは「公開した」と読まず `[ハーネス失敗]` で落とす。固定の `sleep 0.3` は消した
  - ゲートを開ける前に「bg がまだ生きている」を固定した (窓を作れていないと判定が素通りする)
  - **負荷任せの窓を決定論にした**: bg が使う `rm` を「0.5 秒待ってから本物 (絶対パス) を実行する」shim に差し替える。
    固定の sleep で待つ実装はこの窓で必ず落ち、bg の終了を待つ実装は通る
- 結果 (実測):
  - 旧版の待ち方: 並列 16 本 × 3 回 = 48 回で**再現 0 回** (負荷だけでは起こせなかった)。旧版の待ち方に rm の shim だけを
    足すと、issue と同じ `✗ 親の死後に 1 件公開した: …result.partial` で必ず落ちる (= 仮説「bg が途中のまま判定した」の裏付け)
  - 新版: 単独 3 回連続 green、並列 16 本 × 3 回 = 48 回で失敗 0 回。残骸プロセス (shim の待ち) 0 件
  - 変異: `_dotfiles_check_bg` の `if kill -0 "$owner"` を `if true` にすると `✗ 親の死後に 1 件公開した: …result` で red (bin/mutate-verify rc=0)
- 残タスク: なし

