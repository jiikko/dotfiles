# 754 (research): glogx の issues viewer の監査 (設計 7・品質 8・テスト 2。2026-10-13。直接実行)

起票日: 2026-10-13

## 概要

`/audit` を直接実行で、glogx の issues viewer (`src/glogx/issues_*.go` と `src/glogx/issues/`。非テストは約 6.3k 行) に回した。
前回 (設計とテストは 10/07 の 668、品質は 10/09 の 698 / 702) 以降に 12 commit・+927 / −567 行が動いたので、その差分を重点にした。
read-only の調査体 3 体 (設計 7 種 / 品質 8 種 / テスト 2 種 = sonnet) を 1 波で並列に回した (ユーザーの指示で、次回からは直列)。
main は主要な主張をコードで確かめ、品質の再現テストを自分でも走らせた (5 本 FAIL)。

## 結論 (起票先)

| 起票先 | 中身 | 今壊れているか |
|---|---|---|
| 748 | next から出す移動の失敗で claim が半端に外れる (P2) / バナーの front matter の判定が LoadMeta と違う (P3) | 前者は壊れている・後者は潜在 (どちらも複製で再現。main が再実行) |
| 749 | 見張りの基準を読み終えた後に取り、走査中の編集を拾わない (P2。K1) | 壊れている (低頻度。監査体の掃引で 1 回) |
| 750 | n の確認中も hint 行が一覧の案内のまま (P2) | 壊れている (main が `hint` と `markNextKey` のコードで確認) |
| 751 | 変異で緑のままのテスト 5 項 + 手組みの寄せ | 監査体の変異 |
| 752 | 番号フィルタの終わらせ方の分散 (E2) / epic の子の判定の写し | 潜在 |
| 753 | コピーするパスの制御文字 / NextNumber の範囲 | 未確認 |

全数勘定: 採用 14 項目 = 設計と品質で 8 (設計 4・品質 5 のうち front matter の判定は両方が出したので 1 件に統合) + テスト 5 + test-helpers 1。却下は下節の 10 項目と 751 の却下節 (3 項目)。

## 却下した指摘と理由 (再提起の前に読む)

668・698・702 の却下はそのまま有効。今回の分:

- `openDoc` が doc を `issues.Issue` に包み、`Rel` に表示用のラベルを入れる: 666 の D (docPager 型。trigger 待ち) と同じ根。今の経路で Rel を同一性に使うのは `copyNumber` の basename くらいで、実害なし
- `pruneExpandedGroups` が `v.groups.manual` を直接読む: 長さで早く抜けるためだけ。書き込みは型の中に閉じている
- `issues.OrderLess` の引数 6 つの取り違え: 呼び出し 2 か所でテストあり
- `stopGlides` / `advanceGlide` / `animating` の滑走の列挙: 666 F でコメントに縛った範囲
- `issuesWatchDirs` が Scan と別に木を歩く: 状態名は同じ表を引き、symlink と skipDirs の扱いも一致。epic 名の EqualFold が 4 か所あるが、大文字小文字で別の dir を持てるのは Linux だけ (対象外)
- `drainWatchEvents` の `if !timer.Stop() { <-timer.C }`: go 1.25 の同期化された timer ではブロックしない
- `readDirWarn` の警告が見張りのループで鳴り続ける: 警告は走査ごとに作り直すだけで指紋は動かない
- `urlRe` が全角の閉じ括弧や続く日本語まで URL に含める: ~/src の 9 repo の issues の実データで 0 件 (監査体の数え)
- `copyToClipboard` が Update の中で同期実行: glogx 全体の設計で viewer 固有ではない
- `MoveToSubdir` の Stat → Rename の TOCTOU: 窓が狭く、既出の「上書きしない」の範囲
- assert の無い Test 関数・`issues_epic_status_dirs_test.go`・時刻 / XDG の共通化: 751 の却下節

## 攻めたが見つからなかった範囲

- data race: 複製で `go test -race` (issues 関連に絞る) rc=0
- dead-code: `staticcheck -checks=U1000 -tests=false` の報告は allowlist 済みの `zoom.go (*appZoom).start` だけ。`issues` の exported 47 個に production 参照 0 のものなし (grep の粗い数え)
- 666 で寄せた型 (`groupExpansion` / `watchChain` / `stopGlides` / `promptWindow`) の field へ外から書く迂回: 0 件 (grep)
- 665 で寄せた規則: viewer 側に状態名のリテラルや並び順の写しは残っていない
- 見張りの世代・札・release、closed のときの縮退、`issues_state.go` の原子的な書き込みと TTL、URL を開く引数 (`https?://` 始まり)
- 変異で赤になった (効いている): `move.go` の目印の戻しとバナーの解除の失敗の戻し
- 通読のみ (変異なし): linkjump / watch / close_anim / group_view のテスト、`issues/{parse,filelink,banner,untrusted,discover}_test.go`、tests/issues/*.sh (前回以降の変更なし)
- performance: 新しい発見なし (実測もしていない)

## 進捗

- [x] 監査を実行し、発見を 748〜753 に起こした
- [ ] 起こした issue の反証レビュー
