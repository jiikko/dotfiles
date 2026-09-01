# xcodebuild 実行中に同一 checkout の SwiftPM で swift build/test を並行させない

> **トリガー型ルール。** `make test` / `make build` (xcodebuild) を background で走らせたまま、
> 同じ checkout のローカル package に対して `swift build` / `swift test` (変異検証・別検証)
> を打とうとした瞬間に発動する。

## ルール

- **xcodebuild (test/build) の実行中は、その project が参照するローカル SwiftPM package で
  `swift build` / `swift test` を並行実行しない**。xcodebuild の package graph 解決
  (Reload Package) と SwiftPM の build/cache が競合し、**エラーではなく無限待ちで固まる**
  (実測: obaket で `Reload Package 'shared'` のまま 7.5 時間停止。ユーザー指摘まで気づけず)
- 変異検証などで並行したいときは、**xcodebuild の完了を待つ**か、**変異側を使い捨て worktree**
  (別 checkout = 別 .build / 別 package graph) に出す
- **kill した xcodebuild run の直後の full run は判定に使わない**。残骸環境でテストが
  flake する (実測: kill 直後の run だけ drop 系 6 本 red、単独・再実行は green)。
  kill 後は 1 回捨て run を挟むか、単独 suite 再実行で切り分けてから判定する
- **スタックの検出**: background の xcodebuild が長い時は log の tail と mtime を見る。
  「`Reload Package` が最終行のまま mtime が数分止まっている」がこの競合のシグネチャ

## なぜ

起源: obaket issue 651 M5, 2026-09-01。xcodebuild `make test` の実行中に、shared package で
変異検証の `swift build`/`swift test` を並行させたところ、xcodebuild 側が package reload で
ロック待ちに入り無限停止した。プロセスは生存し log も「進行中」に見えるため、
完了通知ベースの運用では永遠に気づけない。

## やること / やらないこと

- ✓ xcodebuild 実行中は同一 checkout での swift build/test を完了まで待つ (または worktree へ)
- ✓ 長い xcodebuild は log の最終行 + mtime でスタックを検出する
- ✓ kill した run の直後の結果は判定に使わず、切り分けてから読む
- ✗ 「別 target だから大丈夫」と同一 checkout で並行させる (package graph は共有)
- ✗ 「まだ走っている」を「時間がかかっている」と読み続ける (mtime を見る)

## 関連

- [`parallel-write-agents-need-worktree-isolation.md`](parallel-write-agents-need-worktree-isolation.md) — 同一 working tree を複数主体が使う問題の一般形 (こちらはビルドシステム版)
- [`verify-execution-not-just-exit-code.md`](verify-execution-not-just-exit-code.md) — 「まだ走っている」と「壊れている」を混ぜない読み方
- 起源の記録: obaket `issues/672-retro-upload-byte-source-651-2026-09-01.md` (項目 1/3)
