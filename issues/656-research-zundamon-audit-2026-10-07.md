# 656 (research): zundamon-kaisetsu (Go) の監査の記録 (2026-10-07、16 観点)

起票日: 2026-10-07

## 概要

`src/zundamon-kaisetsu` を 16 観点 (design / responsibility / duplication / polymorphism / leaky-abstraction / encapsulation / ui-components /
security / resource-leaks / broken-code / dead-code / error-handling / false-green / performance / test-cleanup / test-helpers) で監査した。
直接実行。read-only のサブエージェント (opus) 3 本を直列に走らせ、発見は main が実コードで裏を取ってから issue にした。

- 母集合: テスト以外の .go 11 本 (3,950〜4,210 行。数え方で幅がある)・関数 170 個、`_claude/skills/zundamon-kaisetsu/templates/player.html` (264 行)、
  テスト 97 本 (`^func Test`。サブテスト込みで `go test -race -v` の RUN 128 / PASS 128 / SKIP 0)
- 道具: `go vet` rc=0 / `make lint` 0 件 / golangci-lint に gosec・gocritic 等を足した走査 (製品コードで gosec 36・gocritic 3・errorlint 1。下の所見以外は意図どおりか該当なし)

## 全数勘定

| 起票 | 中身 | 重さ |
|---|---|---|
| 649 | mermaid の中断・時間切れで detached の Chrome が残る | P2 |
| 650 | Go と player.html の契約 (`#sheet=`・図解の種類と読むキー) が検査されていない | P2 |
| 651 | writeMP4 の組み立てにテストが無い | P2 |
| 652 | synth の古いファイルの数え方 / 中断の理由の不揃い / 出力先の確認が遅い | P3 ×3 |
| 653 | 死んだコード・古いコメント / golden が空でも通る / wav ヘッダ | P3 ×6 |
| 654 | ファイル構成と重複 | P3 ×6 |
| 655 | テストのヘルパー / 性能の小さな改善 (アセンブラは使わない) | P3 |

## 却下・記録のみ (再提起には実測を添えること)

- **security: `show.image.src` が台本の dir の外を指せる** → 立ち絵の `cast.*.faces` と同じ扱いで、台本を信頼する設計。害は手元の PNG / JPEG で縦横の検査を通るものに限られる。
  他人の台本を build して HTML を共有する運用が出てきたら「dir の外を指したら警告」を検討する
- **leaky: 図解の文字数の上限が player.html の CSS の文字の大きさから実測した値** → show.go のコメントに「変えたら測り直す」とあり、手で保つと決めた意図がある
- **error-handling: `--format both` で mp4 が失敗すると新しい html だけが残って rc=1** → 失敗は報告されるので害が小さい
- **encapsulation: 台本の行が `map[string]any`** → Python 版と同じ順に値を引く意図的な選択 (Script のコメント)。654 に注記
- **dead-code: G115 (`writeWav` の uint32 変換)** → 約 24.8 時間を超える音声でしか溢れない
- **test-cleanup**: 削除すべきテストは無い (引数の解析のテストの重なりは、各本が別の分岐も見ている)
- **performance: アセンブラ / SIMD** → 使わない (655 に数字)

## 攻めたが見つからなかった範囲

- player.html への注入 (台本由来の値は textContent / alt / data URI の img.src。innerHTML は不使用。`__DATA_JSON__` と `__TITLE__` のエスケープ)、外部コマンドへの注入 (すべて exec の引数)
- ファイル・HTTP Body・一時ディレクトリ・ロックの解放、flock の fd の継承 (CLOEXEC)、writeMP4 の goroutine とセマフォ
- wav の chunk の読み方・mouthTrack / frameRuns の境界・ffconcat の最後の長さ・readings の置き換え・JPEG の EXIF の境界
- 製品コードで、製品コードから参照されない関数は 0 個 (lint の unused も 0 件)
- engine_auto のロックの判定不能を「使用中」に倒す作り、signal / interrupt の結合テストに skip が無いこと

## 反証レビュー (2026-10-07、read-only のサブエージェント 1 本)

P1 / P2 なし。P3 2 件を訂正した (655 の手書きの台本 3 → 4 か所・testdata を写す処理 3 → 2 か所 / 656 のテストの本数はサブテスト込み)。
650 A の「全部同じ絵の検査で捕まらない」は Chrome が要るので、反証も裏付けもできないまま (未実測のまま扱う)。

## 進捗

- [x] 649〜655 の対応 (2026-10-07 に全部 done。各 issue に結果と敵対的レビューの記録)
- [x] `src/zundamon-kaisetsu/README.md` / `CLAUDE.md` を新設 (ユーザーの依頼)
