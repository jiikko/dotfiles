# 216 bug: glogx の unit test が doctor の実走査 goroutine を join せずに漏らす

起票日: 2026-09-03
出典: issue 214 (collectVisits のデータ競合) の敵対的レビュー (opus, red team)。
      214 の「なぜ起きたか」より上流の発火源として見つかった
重要度: P2 (CI の赤の再発源。実データ・実 brew / xcrun をテストが叩いている)
関連: `src/glogx/doctor_view.go` の `start` / `stop`、`src/glogx/tui_helpers_test.go` の
      `t.Cleanup(m.cancel)`、`src/glogx/doctor_cleanup.go` の `doctorCleanup` latch、issue 214 / 211

## 症状

glogx の unit test が、**実マシンに対する disk 走査**を起こし、その goroutine を join せずに
次のテストへ跨がせる。issue 214 のデータ競合 (`collectVisits`) は、この漏れた goroutine が
**たまたま跨いでいた唯一の package 変数**を踏んだ結果で、214 の atomic 化はその 1 個を
黙らせただけ。doctor 側に package 可変状態が 1 つ増えた瞬間に同じ赤が戻る。

## 再現 (実測 2026-09-03)

```
shim に brew / xcrun を置いて PATH 先頭 (実体は絶対パスで exec)
cd src/glogx && PATH="$shim:$PATH" XDG_CACHE_HOME=$(mktemp -d) \
  go test -race -count=1 -run TestUpdateKeysReachableFromOverlays .
→ shim log: "xcrun simctl runtime list -j" / "brew --prefix" / "brew info --json=v2 --installed"
```

## なぜ起きるか

- `tui_update_keys_test.go` の `openByKey("D", …)` は seam を差していない `newTestBrowse` 上で
  `doctorOv.toggle()` → `start(false)` を通す。`start` は seam が無ければ
  `disk.Options{Env: disk.RealEnv(), Run: runner.Exec}` を使う (`src/glogx/doctor_view.go` の
  `dOpt` 初期化)。つまり**実 /Applications・実 brew・実 xcrun**を走らせる
- 後始末は `t.Cleanup(m.cancel)` (`src/glogx/tui_helpers_test.go`) → `doctorOv.stop()` で、
  これは **cancel だけで join しない** (`stop()` のコメント自身が「cancel だけでは子プロセスの
  死を待たない」と書いている。issue 211)。テストは 0.24 秒で終わるので走査は生き残る
- **「手元では緑」の説明も同じ経路にある**: `start` は `!force` のとき snapshot があれば
  走査せず即 return する。開発機に snapshot があれば漏れず、CI (新品 cache) では必ず漏れる

## 併発する不変条件の破れ

`.github/workflows/src_doctor.yml` のヘッダは「実データ・実 launchd は触らない (テストは偽 HOME と
fake runner)」を doctor 側の不変条件として書いているが、**同じコードを glogx.test が実 runner で
叩いている**ので glogx 側で破れている。

## 直し方 (どちらか)

- `newTestBrowse` に doctor の seam (fake runner + 偽 Env) を**既定で**差す
- `t.Cleanup` を `stop()` ではなく `doctorCleanup` latch の join (`waitDoctorCleanup` 相当) にする。
  latch は既にあるので数行

🚨 **`src/glogx/doctor_view.go` / `src/glogx/tui_helpers_test.go` はユーザー指示で凍結中**
(2026-09-03。ユーザーが別マシンのセッションで全体見直し中)。**合図が出るまでどのセッションも
触らない**。同じ凍結指示は dotfiles-92 にも届いており、そちらが凍結直前に入れていた
doctor_view.go の 3 箇所は revert され、patch が `issues/pending/169` の末尾に退避されている
(合図後に見直し後の実装と突き合わせて当てる方針)。着手時はその patch との衝突を先に見る。

## テスト観点

- shim (brew / xcrun を PATH 先頭、実体は絶対パスで exec) を置いて `go test` を回し、
  **shim が 1 度も呼ばれないこと**を回帰テストにする
- 変異: seam を外すと shim が呼ばれて red になること

## レビュー状態

出典が opus の敵対的レビューで、再現手順・`file` 参照は main agent が裏取り済み
(`doctor_view.go` の `RealEnv()` / `stop()` の非 join / snapshot による早期 return を HEAD で確認)。
反証レビューは未実施。


---

## 決着 (2026-09-03)

commit `22995adc` (修正) / `5ba45b9c` (回帰テスト) / `5c1f736d` (敵対レビューの反映) /
`6adcc02f` (lint)。

### 実測

| | 修正前 | 修正後 |
|---|---|---|
| glogx のテストが叩く実コマンド | **29 回** (`xcrun` / `brew` / `pgrep`) | **0 回** |
| 全テストの所要 | 39s | 32s |

⚠️ **issue の想定より入口が広かった**。issue は「`D` で開く経路」を挙げていたが、
一番の発生源は**削除の結果パネルを閉じる経路** (`doctorRescan` → `rescan()`) で、
キーを 1 つ押しただけで実走査が始まっていた (20 回 / 29 回)。

### 直したもの

- `newTestBrowse` と `realRepoBrowse` (**工場は 2 つあった**) が `installInertDoctor` で
  doctor の走査口を fake に差し替える。カタログを空にするのが要点 (エントリ 0 件なら Run も
  呼ばれない)
- 後始末を `m.cancel` から **`m.cancelAll` + 上限つきの join** へ。前者は `doctorOv.stop()` を
  呼ばないので走査が止まらず、上限なしの `waitDoctorCleanup()` と組むとパッケージごとハングする
  (敵対レビューが 45 秒 timeout の panic を再現)
- 回帰テスト `tests/glogx/test_no_real_commands_in_tests.sh` (PATH shim で 0 回を確認)
- **CI に `no-real-commands` job を新設** (`src_glogx.yml`)

### 🚨 敵対的レビュー (opus) が出した、私の最大の見落とし

**回帰テストが CI で 1 度も走っていなかった。** Tests workflow の runner に Go が無く
`exit 77` で skip、Go がある `src_glogx.yml` は `go test` しか回さない。私がヘッダに書いた
「CI の src_glogx.yml が本番」は嘘だった (レビューが実際の CI ログで実測)。
`~/.claude/rules/adversarial-review-own-safeguards.md` §4 に正面から抵触。

他に、空振り (`-run` が当たらない) が緑になる / エラー経路そのものが
`$min_pass。` の全角で壊れていた (失敗の理由が読めない赤になる) も同レビューで判明。

### 採用しなかった / 記録に留めた指摘

| 指摘 | 判断 |
|---|---|
| `Catalog: []disk.Entry{}` を `nil` にする変異が緑 | **記録**。実際の防波堤は `Env.Home = t.TempDir()` 側で、コマンド数の検査は「実 FS を走査し始めた」を原理的に検出しない。空/nil の区別は `installInertDoctor` のコメントが正本 |
| `inert` の `launchctl` 分岐が観測不能 (空文字を返しても結果が同じ) | **記録**。契約に合わせた飾りだが害は無い。`svcOpts` を外す変異では実 `launchctl` が出るので、seam 自体は守られている |
| shim を全実行ファイルに広げる | **採らない**。1656 本の shim はレビューの検証手法としては有効だが、常設の検査としては重い。doctor の接触面 (brew / xcrun / pgrep / launchctl / du) に絞る |

### 残した観測ポイント

`waitDoctorCleanup` は production 用で、テストから呼ぶと「終了を待っています」が出力に混ざる。
今は上限つきの `joinDoctorCleanup` で包んだが、production 側の関数をテストが直接使う構造は残っている。
