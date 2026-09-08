# 331 feat: issue の状態に `waiting/` (着手済み・事象待ち) を足す

- 起票 / 完了: 2026-09-08
- 発端: obaket issue 742 (「これって再現待ちだよね？」→「pending とは別のステートだと思う」)

## 何が足りなかったか

`issues/` の状態は `open` / `next` / `pending` / `done` の 4 つで、
**「着手済みだが、こちらから起こせない事象を待っている」issue の置き場が無かった**。

- `pending` は**凍結**。「着手条件・trigger 待ち」で、再開の主導権は**自分**にある
- 一方 obaket 742 は段階 1 / 1b を**実装して push 済み**で、残るのは
  「次に同じ失敗が出たときのログ 1 行」を待つことだけ。再現条件が分からないので**起こせない**
- open に置くと「今やれること」の一覧に作業待ちとして並び続ける

同じ状態の issue は obaket に起票時点で **3 件** (742 / 543 / 546)。いずれも本文に
「観測待ち」と書いて `issues/` 直下に open で置かれていた = 一回限りの状況ではない。

## 過去の決定との関係

`docs/issues-viewer-spec.md` §4 は **`ongoing/` を作らない**決定をしている。取り消していない。
反対理由が waiting に当たるかを 1 件ずつ見た:

| `ongoing/` を作らない理由 | waiting に当たるか |
|---|---|
| 記帳する時間が無い (作成→done が 4〜24 分) | **当たらない**。waiting の滞在は無期限 |
| 1 ファイル 1 ディレクトリでは表せない (pending の 2 件は「着手済み・一部完了・trigger 待ち」を同時に満たす) | **当たるが、それが分ける理由になった**。その 2 件がまさに waiting 側で、pending の語義が実態と乖離していた |
| 採番コマンドの視界外になる (「作るなら先に全ディレクトリ走査へ直せ」) | **解消済み**。obaket の `bin/next-issue-number` は再帰走査で、`waiting/` に 999 を置いたら 1000 を返すことを実測 |

## 入れたもの

- `src/glogx/issues/parse.go`
  - `StatusWaiting` を enum の**末尾**に追加 (途中に入れると既存の並びが動く)
  - `String()` = `waiting` / `Badge()` = `◌` (○ と同じ Geometric Shapes ブロック。
    中身が抜けた ○ = 「open だが今は動かせない」)
  - `statusDirs["waiting"]`。綴りの揺れ (`hold` / `on-hold` のような別名) は受けない
  - `shows()` で **pending と同じ段** (`FilterPending`) に置く。段階を増やさないのは
    巡回が 1 キーでヒント行が 1 行しかないため。バッジが別なので一覧では区別できる
- `docs/issues-viewer-spec.md` — 状態表・group 制限・§4 との関係
- `issues/README.md` — pending との違い (再開の主導権が誰にあるか) と書くべき内容

## やらなかったこと

- **group 内 (`epic/<name>/waiting/`) は未対応**。受けない名前の配下は**走査対象外 = 一覧に出ない**
  ので、入れるなら `EpicChildStatus` / `scanEpicDir` / `hasEpicMarkdown` / `issuesWatchDirs` の
  4 者を揃える必要がある (`TestEpicChildStatusReachedByAllThreeConsumers` が 3 経路を守っている)。
  制限として spec / README に明記した
- **フィルタ段階は増やしていない** (open → pending → all の 3 段のまま)
- `ongoing` (着手中) は引き続き作らない

## 検証

- `go test ./...` (glogx 全 package) green
- 新テスト 3 本が実際に走った証拠を `-run Waiting -v` で確認
- **変異検証** (性質ごとに 1 本、いずれもビルド成功を確認):

  | 変異 | 結果 |
  |---|---|
  | `statusDirs` から `waiting` を外す (パス写像を落とす) | `TestScanReadsWaitingFromDirectory` が red |
  | バッジを pending と同じ `⏸` にする | `TestWaitingIsSeparateFromPending` が red |
  | `shows()` から `StatusWaiting` を外す (既定で見えてしまう) | `TestWaitingIsSeparateFromPending` が red |

- 🚨 最初は共有 fixture に waiting のファイルを足したが、**同じ fixture で件数を固定している
  別テスト 2 本が落ちた** (`bug` / `other` のタブ件数が動く)。専用 fixture に切り替えた。
  共有 fixture にファイルを足すときは、件数を assert しているテストを先に数えること

## 残タスク

なし。group 対応は必要になってから (上の「やらなかったこと」に条件を書いた)。
