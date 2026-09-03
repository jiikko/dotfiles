# 220 feat: プロジェクト配下のビルド成果物・node_modules を doctor で扱えるようにする

起票日: 2026-09-03
出典: [issues/218](218-feat-doctor-catalog-and-threshold-decisions.md) の D 節 (別 issue が妥当と判断した分)
重要度: **P3** (現行カタログの枠組みに乗らないため、設計から要る)

## なぜ現行カタログに載せられないか

`src/doctor/disk/catalog.go` は **allowlist** で、「このパスは消しても戻る」を実測してから
登録する形になっている。`~/src/**` のビルド成果物と `node_modules` はこの形に乗らない:

- **パスが固定でない**。プロジェクトごとに `node_modules` / `target` / `build` / `dist` /
  `.next` / `vendor` が散在し、glob では「消してよいもの」と「消してはいけないもの」を分けられない
- **消してよいかがプロジェクト単位で違う**。`node_modules` は `package-lock.json` があれば
  復元できるが、lock が無い / private registry に依存している / ネットワークが無い環境では戻らない
- **復元コストが桁違い**。カタログの Tier 1 は「次回ビルドで再生成」だが、こちらは
  依存解決のやり直し (数分〜十数分) になる

## 扱うなら要る設計

- **プロジェクトを単位にする** (パスではなく)。`package.json` / `Cargo.toml` / `go.mod` の
  在るディレクトリを探し、その中の成果物を「そのプロジェクトの分」としてまとめる
- **最終更新で絞る**。「N か月触っていないプロジェクトの成果物」なら復元コストを払う覚悟がしやすい
- **復元手段が在ることを確認してから出す** (lock ファイルの存在 / registry が公開か)
- 削除は `DeleteVia: propose` (コマンドを提示するだけ) から始めるのが安全

## 未実測

この機の `~/src` 配下の実サイズを測っていない。**着手時に最初に測る**
(現行カタログの合計 84.6GB に対してどれくらいか分からないと、優先度が決まらない)。

## 受け入れ条件

- [x] `~/src` 配下の成果物・`node_modules` の実サイズを測り、issue に残す (2026-09-03。下記)
- [ ] プロジェクト単位でまとめる設計を書き、カタログ (allowlist) との関係を決める
- [ ] 復元手段の確認方法を決める (lock ファイル / registry)

## 実測 (2026-09-03、この機の `~/src`)

**合計 19.4 GB** (現行カタログの合計 84.6GB に対して約 23%)。
プロジェクト数: `package.json` 46 / `go.mod` 12 / `Cargo.toml` 0。

| 種別 | サイズ |
|---|---|
| `.next` | 6.1 GB |
| `node_modules` | 4.4 GB |
| `build` | 3.8 GB |
| `.build` | 3.8 GB |
| `dist` | 0.7 GB |
| `vendor` | 0.6 GB |

**プロジェクトの最終更新で切ると:**

| 古さ | サイズ |
|---|---|
| 180 日以上 | **8.2 GB** |
| 90〜180 日 | 0.7 GB |
| 30〜90 日 | 2.0 GB |
| 30 日未満 | **7.7 GB** (現役。候補にできない) |

lock ファイル (`package-lock.json` / `yarn.lock` / `pnpm-lock.yaml` / `go.sum` / `Cargo.lock`)
の有無: **有り 27 / 無し 16**。無しはほぼ Swift。

上位 8 件 (日数 / サイズ / lock):

```
  23 日  5848 MB  lock=yes  ./pj_energy_matching/frontend/.next
 241 日  3291 MB  lock=no   ./working/SnapTrim/build
 241 日  2416 MB  lock=no   ./working/SnapTrim/.build
  62 日  1523 MB  lock=no   ./working/swift-smbee/.build
  23 日   901 MB  lock=yes  ./pj_energy_matching/frontend/node_modules
 404 日   766 MB  lock=yes  ./gx-navi/frontend/app/node_modules
 119 日   720 MB  lock=yes  ./working/dropbox-multi-video-player-electron/node_modules
 709 日   553 MB  lock=yes  ./ubiregi-log-viewer/node_modules
```

## この実測が設計を変える点 (「扱うなら要る設計」の節は過剰)

1. **最大の塊は `node_modules` ではなく Swift のビルド成果物**。
   `SnapTrim/build` + `SnapTrim/.build` + `swift-smbee/.build` の 3 件で **7.2 GB = 全体の 37%**。
   これらは**依存解決のやり直しが不要**で次回ビルドで再生成されるだけなので、本文が心配していた
   「復元コストが桁違い (数分〜十数分の依存解決)」は**最大の塊には当てはまらない**。
   `lock=no` なのは Swift だからで、**lock ファイルの確認も不要**
2. **`node_modules` の最大値は現役プロジェクト**。`.next` の 5.8 GB は 23 日前に触っている
   `pj_energy_matching` なので候補にできない。古い `node_modules` は数百 MB 単位
   (404 日 766MB / 709 日 553MB)
3. したがって **「プロジェクト単位でまとめる」「復元手段 (lock / registry) の確認」は
   8.2 GB の回収には要らない**。要るのは `node_modules` を候補に含めたい場合だけ

## 絞ったスコープ (最小実装で 8.2 GB)

- 対象: `~/src` 配下で `build` / `.build` / `target` を持ち、**プロジェクトの最終更新が
  180 日以上前**のもの
- guard: 日数のしきい値 1 つだけ (既存の `GuardBoottime` = mtime で絞る、と同じ考え方)
- `DeleteVia: propose` (コマンド提示のみ) から始める
- `node_modules` / `.next` / `dist` / `vendor` は**第 1 段では対象外**。数百 MB 単位で、
  かつ復元手段の確認が要るため、必要になってから別段として足す

## 受け入れ条件 (実測を踏まえて改訂)

- [x] 実サイズを測って issue に残した (2026-09-03)
- [ ] 上記「絞ったスコープ」で実装する (日数しきい値の guard + propose)
- [ ] `node_modules` 系を含めるかは第 1 段の後に判断する (含めるなら復元手段の確認方法を決める)

