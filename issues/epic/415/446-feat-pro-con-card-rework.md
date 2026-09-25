# 446 (feat): レビューで差し戻す — card rework

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

PM がレビュー待ちのカードを見て「ここを直して」と PG に返す操作が無い (2026-09-25 の dogfooding 1 回目で見つけた。440)。
今ある操作は add / plan / ask / answer / review / close / run / guide / list / show / wait で、レビュー待ち → 作業中へ戻す口が無い。
今は PM が自分で直すか、そのまま取り込むしかない。

## 対応方針 (候補)

- `pro-con card rework <カード> "<直してほしい点>"`: レビュー待ちのカードを作業中へ戻し、同じ PG の session を、直してほしい点を渡して再開する
  (回答 (answer) で再開するのと同じ経路。受付の箱に `rework` の依頼を置き、dispatcher が適用する = 426 の決定 1)
- 履歴に「差し戻した: <原文>」を残す (要約しない)
- pm-guide.md の役目 5 (レビュー) に「直してほしい点があれば rework」を足す
- 差し戻しの回数の上限は設けない (人が見る)

## 進捗

- 2026-09-25 18:42〜18:52: dogfooding の 3 枚目 (カード C-004) として PG が実装した (440)

### 2026-09-25 (C-004 の PG)

やったこと:

- `pro-con card rework <カード> "<直してほしい点>"` を足した。受付の箱に `rework` の依頼 (`Request.Rework`) を置くだけで、適用は dispatcher (`store.transition`)
- 適用: レビュー待ちのときだけ受け、分解済みへ戻して `Resume` に「`store.ReworkPrefix` + 直してほしい点 + もう一度 review せよ」を入れる。
  dispatcher は回答と同じ経路 (`resumes` → `Launch.Resume`) で同じ session を再開する (dispatcher 側は変えていない)
- 履歴は「差し戻した: <原文>」(切り詰めない)。レビュー待ち以外 (作業中・完了等) と、直してほしい点が空のものは断る (rejected/ へ除ける)
- pm-guide.md の役目 5 に rework を、README の card の行に rework を足した

確かめたこと:

- テスト: `store.TestReworkReturnsReviewToPlanned` / `TestReworkRejectsOutsideReview`、`dispatcher.TestReworkedCardResumesSameSession`、
  `TestCardCommandAskAnswer` に rework の行、`TestPMGuideCommandsParse` が guide の rework の行をパーサに通す
- `make test` (テストの係経由) rc=0
- `bin/mutate-verify` で 5 本の変異 (状態の検査を外す / 空の検査を外す / 履歴を 80 文字で切る / Resume を渡さない / パーサで本文を落とす) が、それぞれ狙ったテストだけを red にするのを確かめた

残り:

- 画面 (TUI) からの差し戻しのキーは無い (live backend の `Accepts` も new / answer だけ)。要るなら別 issue
- 実機 (本物の dispatcher と claude --bg) での差し戻し → 再開は未確認
