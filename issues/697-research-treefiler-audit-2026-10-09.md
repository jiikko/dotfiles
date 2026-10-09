# 697 (research): treefiler の監査 (2026-10-09。21 種類を直接実行)

起票日: 2026-10-09

## 概要

`/audit src/treefiler` を 21 種類 (設計 7・品質 8・テスト 2・その他 4) で、読み取り専用の調査体 (sonnet) 5 体を 1 体ずつ順に回した。
各体の発見は main (Opus) が実コードで裏を取ってから issue にした。1 つの発見は一番狭い型に 1 回だけ数えた。

| 調査体 | 監査の種類 | 走査した母集合 | 起こした先 |
|---|---|---|---|
| 1 | design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components | filer/*.go 27 ファイル (非テスト 19) + main.go + glogx の接点 3 ファイル | 691 (5・6・8)・694 |
| 2 | security / broken-code / error-handling / dead-code | filer/*.go 非テスト 25 + main.go + glogx の接点 + subproc・atomicfile・termsafe の入口 | 691 (1〜4・9〜12)・692 |
| 3 | resource-leaks / concurrency / false-green / performance | 共有値を持つ型 6 つ (gitWatch・walker・watcher・opener・diffJob・explodeJob) と CI の lane | 691 (7)・693・695 (2・3) |
| 4 | test-cleanup / test-helpers | filer/*_test.go 8 ファイル (約 90 本) + glogx/filer_view_test.go (14 本)。変異 13 本 | 695 |
| 5 | dependency / ci / lint-from-done / general | go.mod 3 つ・src_treefiler.yml / src_glogx.yml の run・issue 662 / 689 の採用節 | 696 |

## 全数勘定

- 起こした issue: 691 (bug 12 項)・692 (bug 1。glogx を含む repo 全体)・693 (perf 5 項)・694 (refactor 11 項)・695 (test 10 項)・696 (chore 14 項)
- 0 件だった種類: dead-code の到達不能 (`make test-unused-excluding-tests` で 20 module とも 0。未使用の exported は `SearchQuery` だけで 694 の 11)・
  resource-leaks (goroutine・fd・子プロセスの残り)・concurrency の data race (下記)
- test-cleanup: 消してよいと言い切れるテストは 0 件。検知力の弱い `TestHeatStops` を 695 へ

## 攻めたが見つからなかった範囲 (次の監査の起点)

- data race: `go test -race` に加え、ランダムなキー列・F の開閉 40 回・3ms ごとのファイルの作成と削除・git の repo ありの負荷を複製で回して 0 件。
  goroutine 数は戻った。共有値は lock の内側か、公開した map / slice を書き換えない設計
- 注入: `!` の組み立ては `$2` を引数で渡す (こちら側の注入は無い)。`open` と git のパスは `filepath.Abs` 由来で `-` 始まりにならず、`--literal-pathspecs` と
  `--` の位置も正しい。ファイル名・本文・diff・ブランチ名は termsafe を通る。`putSGR` は CSI 以外を落とす
- 設定ファイルの 1 行書き換え・places の壊れた入力・atomicfile の後始末・watcher の close 直後の再 start・git の rename の読み飛ばし
- CI の paths: replace している 4 module と golangci_lint.sh・_go-project.yml は src_treefiler.yml に入っている。treefiler のテストは module の外を読まない
- 却下: 貼り付けの後に `Animating()` が false になる件 (glogx は次の tick・単体は毎 Update の Advance で自己回復するので実害なし)

## 記録のみ (issue にしない)

- `o` が `open` に実行できる種別 (`.command`・`.app`) も渡す。ユーザーの操作が前提で `open` の仕様どおり
- `rerootUp` と設定の読み込みのエラー文が termsafe を通らない (親が読めず、祖先の名前に制御文字があるときだけ。攻撃の経路は成り立ちにくい)
- タイルの `ensure` は開き直す前に通常ファイルか見直さない (Stat から Open の間に FIFO へ差し替える競合が前提)・未来の mtime で `ago()` が負・
  切れたネットワークのマウントで `ReadDir` が固まると walker が止まり Busy が続く

## 進捗

- [x] 監査を実行し、発見を 691〜696 に起こした
