# 085 refactor: glogx のグローバル chrome 合成が 2 箇所に逐語重複 (doc は「一本化」と主張)

起票日: 2026-08-21
種別: refactor
優先度: **P3** (現状ドリフトなし。新しいグローバル overlay を足すと片方で載せ忘れる)

出典: 監査 [071](071-research-design-audit-2026-08-20.md) の `071-chrome-composition-dup` /
`071-two-slide-state-machines` / `071-pager-empty-branch-divergence`。
**出典 issue には「反証で崩れた (却下)」の一覧がある** — 同じ pager 系の指摘のうち
「`job_detail_overlay` が `pagerScrollKey` に委譲していない」「y/N の実行キー述語の 3 箇所独立」は
**反証で崩れている**ので再提案しないこと。offset 書き戻しの非対称は `512b490` で**修正済み**。
`actionModal` の 7 bool 排他は **issue 074 が同じ `action_modal.go` を map 化する予定**なので
ここでは扱わない (応急処置を当てると 074 と衝突する)。

## 確認できた事実 (2026-08-21)

`src/glogx/tui.go` の 2 箇所が、グローバル chrome の合成順を逐語で持っている:

- `finishViewerWindow` (:2885-2903) — centerModal → restartPrompt → usage → toast → `finishWindow`
- `viewLines` の末尾 (:2974-2989) — 同じ 4 つを同順で、同じ引数で

どちらの doc コメントも「ビューごとに書くと片方で載せ忘れる」「前面順もここで一本化する」
「前面順は一覧側 (viewLines) と同じ」と**一本化を主張している**のに、実体は 2 コピー。
過去に「viewer が全画面だった頃、issues 中の通知が画面に一切出ない時期があった」と
コメント自身が記録しており、**この class の事故は既に 1 回起きている**。

## 下がる複雑性

新しいグローバル overlay (次の通知系 UI) を足すときの touch 箇所が 2→1。前面順の契約が
1 箇所になる。呼び出し側は既に `finishWindow` を共有しているので**新しい依存辺は増えない**。

## 対応方針

`viewLines` 末尾の 4 ブロックを `finishViewerWindow` (または共通の
`finishWithGlobalChrome(window, page)`) 経由に寄せる。一覧側には viewer に無い overlay
(job パネル / diff / PR 状態 / スクロールバー) があるので、**寄せるのは最後の 4 つだけ**。

## 付随項目 (価値が下がるので同時にはやらない)

- `071-two-slide-state-machines`: `issuesView.slideAnimating` + `slideInWindow` と
  `statusView.slideAnimating` + `slideLeftWindow` が独立。**演出自体が意図的に別**
  (右から流し込む / 左端から板が生える) なので、共通化できるのは progress・closing・
  tickInterval の状態機械部分だけ。得られる削減は小さい
- `071-pager-empty-branch-divergence`: pager 3 面の「loading / 空 / 本文」の組み立てが独立。
  中心だった offset 非対称は既に修正済みなので、残るのは見た目の統一だけ

## 変異検証

寄せた後、**片方の経路だけ chrome を落とす変異**で red になることを確認する
(= 「viewer で通知が出ない」を捕まえるテストが存在するか。無ければ 1 本足す。過去に
実際に起きた事故なので回帰テストの価値は高い)。

## trigger

グローバルな overlay / 通知 UI を次に足すとき。単独でも小さい (機械的な寄せ + テスト 1 本)。
