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
- テストのケース 6 は、`.partial` が消えるのを最長 10 秒 (`sleep 0.05` × 200) 待ち、さらに `sleep 0.3` してから、`.result` と `.result.partial` の
  どちらかが在れば「親の死後に公開した」とする
- **仮説 (未確認)**: 負荷の下で、bg が 10 秒以内に `rm` まで届かなかった。待ちが時間切れになっても失敗として区別せずに assert へ進むので、
  「bg がまだ書いている途中」を「親の死後に公開した」と読んだ。実装の退行ではなく、ハーネスの待ち方の問題
  - 反対の可能性: bg が `kill -0` で死んだ親を「生きている」と読んだ (pid の再利用)。その場合は `.partial` ではなく `.result` が残るはずなので、
    今回の出力とは合わない

## 対応方針

- 待つ対象を「`.partial` が消えた」から「bg のプロセスが終わった」へ移す (`avoid-wall-clock-assertions.md`: 待ちたい事象そのものを待つ)。
  bg の pid が取れないなら、`.partial` が消えるのを待つ上限を延ばし、**時間切れはハーネスの失敗として別に出す** (公開した、と読まない)
- `sleep 0.3` (rename が起き切るまで) も、bg の終了を待てれば要らなくなる
- 直したら、負荷の下 (例: 並列で CPU を埋める) で繰り返して落ちないことと、`_dotfiles_check_bg` の `kill -0` の分岐を外す変異で red になることを確かめる

## 関連ファイル

- `tests/zshrc/test_dotfiles_check_result_ownership.sh` のケース 6 (「親シェルが先に終わっていたら bg は公開しない」)
- `_zshrc` の `_dotfiles_check_bg`

## 進捗

- 2026-09-28 起票。まだ着手していない
