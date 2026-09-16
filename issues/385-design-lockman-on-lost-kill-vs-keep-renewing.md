# `--on-lost=kill` の「最初の判定不能で即殺す」と、381 の「判定不能でも更新を続ける」が正面から矛盾する

起票日: 2026-09-16
カテゴリ: design / priority: medium
対象: `src/lockman/with.go` の `reportRenewErr` (`escalate.Do`) と select ループ
出典: [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) の敵対的レビュー (観点③ 並行・中断)
反証レビュー: 未実施

## 問題

`reportRenewErr` は**最初の**判定不能で `escalate.Do` を撃つ。理由はコードに書いてある:

> fail-closed で子を止める。**次の tick を待たない** — TTL を超えれば他者が引き継ぐので、
> 待つほど二重実行に近づく。

一方 381 の修正でループ側は逆を言うようになった:

> 判定不能は一過性かもしれないので**更新をやめない**。

**381 より前は整合していた** (諦める = 更新も止める、が一体だった)。381 の後は
**ループが粘り、昇格は即殺す**。昇格は不可逆、更新の粘りは可逆なので、既定
(`--on-lost=kill`) では 381 の便益が**ほとんど回収されない**。

## 実測 (レビュワー。本セッションでは追試していない)

`TestLeaseSurvivesTransientRenewBlock` と**同じ** 900ms の一過性の詰まりを既定
(`onLostKill=true`) で作ると **exit=125 / 子は完走しない**。lease は誰にも奪われていない
(同じ手順の warn 版テストが lease の生存を証明している)。つまり
**生き残った lease のために、殺さなくてよかった子を殺している**。

便益がゼロではない点は正確に: TERM を trap / 無視する子は猶予 (既定 5s) のあいだ走り続け、
その間は更新の粘りが lease を保つ。SIGTERM で素直に死ぬ子では観測できない。

## 判断すべきこと

「判定不能」で即殺すのは**安全側だが高価**。lease を保てる見込みがあるなら、
`renewLost` (確実な喪失) と `renewIndeterminate` (判定不能) で昇格の扱いを分ける余地がある。

- (a) 現状維持。fail-closed を最優先し、381 の便益は `--on-lost=warn` 専用と割り切る
- (b) 判定不能では**猶予を置く** (次の更新が成功したら昇格しない / N 回連続で判定不能なら昇格)。
  🚨 これは fail-closed → fail-open の変更なので、**二重実行の窓を自分で開けることになる**。
  変えるなら A-B の実測と変異検証が要る
- (c) 判定不能のときだけ `--on-lost` の既定を変える (新しいモード)

## テストの穴 (どの案を採るにも先に埋まっていた方がよい)

lease 生存系のテストは**全部 `onLostKill=false`**。既定の経路を通るのは
`TestOnLostKillEscalatesToSigkill` だけで、そこは「本当に喪失した」列。
381 のレビュー観点② で `escalate.Do` のゲートを外す変異が全緑だったのも同じ穴の別の顔。

## 残タスク

- [ ] (a)/(b)/(c) を決める。(b) を採るなら A-B と変異検証を必須にする
- [ ] 既定 (`--on-lost=kill`) の経路を通る lease 生存テストを 1 本足す
- [ ] レビュワーの実測 (一過性の詰まり + 既定で子が完走しない) を追試する

## 関連

- [381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) — 出典。ループ側を「粘る」へ変えた
- [384](384-bug-lockman-escalation-burns-out-and-sigkill-skips-recheck.md) — 昇格**実装**側の穴
- [356](done/356-bug-lockman-with-releases-lock-while-grandchildren-run.md) — 昇格そのものの出典
