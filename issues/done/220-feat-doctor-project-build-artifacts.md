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

## 実測 (2026-09-04)

`~/src` は **54.4GB**。現行カタログの合計 84.6GB と同じ桁で、無視できる規模ではない。

### 成果物ディレクトリの種類別 (最上位のものだけ。入れ子は親に含める)

| 種類 | 合計 | 個数 |
|---|---|---|
| `.next` | **6.11 GB** | 4 |
| `node_modules` | **4.38 GB** | 17 |
| `build` | 4.10 GB | 262 |
| `dist` | 0.99 GB | 912 |
| `vendor` | 0.56 GB | 27 |
| `target` / `.venv` / `__pycache__` | 0.08 GB | 15 |
| **合計** | **≈ 16.2 GB** | |

### 🚨 成果物は 54.4GB のうち 16.2GB でしかない

残り約 38GB は成果物ではない。直下の内訳 (上位): `working` 20.6 / `pj_energy_matching` 12.8 /
`ubiregi-server` 6.3 / `gx-navi` 5.4 / `ubiregi` 2.6 GB。`.git` は **65 repo で 3.18GB**。
**この issue が扱えるのは 16.2GB までで、残りはソースと履歴**。優先度の判断材料になる。

### プロジェクト単位 (100MB 以上の成果物を持つもの)

| プロジェクト | 成果物 | 最終更新 | 復元手段 |
|---|---|---|---|
| `pj_energy_matching/frontend` | **6.59 GB** | **1 日前** | yarn.lock |
| `ubiregi-server` | 0.93 GB | 1 日前 | package-lock.json |
| `gx-navi/frontend/app` | 0.92 GB | **404 日前** | yarn.lock |
| `working/dropbox-multi-video-player-electron` | 0.70 GB | 119 日前 | package-lock.json |
| `ubiregi-log-viewer` | 0.65 GB | **709 日前** | pnpm-lock.yaml |
| `monthly_hours_manager` | 0.60 GB | **841 日前** | package-lock.json |
| `working/good-chrome-extensions/dropbox-video-player` | 0.11 GB | 308 日前 | package-lock.json |
| `working/convenient-link-extension` | 0.10 GB | 408 日前 | package-lock.json |

### この実測が設計に与える答え

1. **「最終更新で絞る」は必須**。最大の 6.59GB は**1 日前に触った現役**で、消すと次のビルドが
   数分〜十数分かかる。逆に **1 年以上触っていない 3 件で 2.17GB**、119 日以上まで広げると
   **4.08GB** になる。ここが実際に狙える範囲
2. **lock ファイルは全件に在った** (8/8)。「復元手段の確認」は実装できるが、この機では
   絞り込みの役に立たない (全部通る)。private registry / ネットワーク不通のリスクは残るので
   検査自体は要る
3. **`dist` は数だけ多くて量が小さい** (912 個で 0.99GB)。プロジェクト単位でまとめないと
   912 行が並ぶ。issue の「プロジェクトを単位にする」判断は実測でも正しい
4. **`.next` が最大の種類** (6.11GB / 4 個)。Next.js のビルドキャッシュはプロジェクトに
   1 つで巨大という形なので、`node_modules` と同じ扱いでよい

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

