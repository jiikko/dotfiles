# lockman Cleanup の selfToken ガードは production 到達不能で、配線しても守るものが無い

✅ **対応済み (2026-09-11 / dotfiles-53)**。推奨対応 A を実施。下の「実施結果」節。

起票日: 2026-09-11
カテゴリ: refactor / priority: low
対象: `src/lockman/cleanup.go` の `Locker.Cleanup` / `main.go` の `dispatch`
出典: resource-leaks 監査 2026-09-11（[issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md)）
反証レビュー: 1 周実施。**起票時の根拠（「1 時間より古い自分の残骸は原理的に存在しない」）は
崩れた**。結論（削除してよい）は別の根拠で生き残っている。下の「配線しても守るものが無い」節

## 問題

`Cleanup(force bool, selfToken string)` は sweep の中に

```go
// 自分の残骸は消さない (自分の acquire が進行中かもしれない)。
if selfToken != "" && (e.Name() == selfToken || e.Name() == selfToken+".json") {
    continue
}
```

を持つが、**production の呼び出しは 1 箇所で、`selfToken` は常に空文字**:

| 呼び出し | selfToken |
|---|---|
| `main.go` の `dispatch` の defer（production の唯一の呼び出し） | `""` |
| `cleanup_test.go` の他 7 箇所 | `""` |
| `cleanup_test.go` の `TestCleanupKeepsOwnScratch` | **実トークン（ここだけ）** |

つまり `selfToken != ""` の分岐は **production から到達不能**で、
`TestCleanupKeepsOwnScratch` が唯一の実行者。
[issue 315](done/315-test-unused-includes-tests-so-production-unreachable-code-stays-green.md) /
[issue 317](done/317-test-termsafe-regression-test-observes-production-unreachable-surfaces.md)
と同型（テストが production 到達不能な面を観測して緑を維持している）。

## 配線しても守るものが無い（ここが要点。3 点で成立する）

### (i) `probe` 側の照合形は構造的に一致しえない

`serverNow` の probe 名は **acquire の token とは無関係な新しい乱数**:

```go
name := filepath.Join(l.metaDir, probeDirName, mustToken())   // lock.go の serverNow
```

ガードの照合は `e.Name() == selfToken || e.Name() == selfToken+".json"`。前者（裸トークン
= probe エントリの形）は、probe が独立乱数で命名される以上**一致しえない**。
ガードが理論上届くのは `tmp/<meta.Token>.json` **だけ**。

### (ii) 届く 1 件（tmp）は、holder にとって消えても無害

`tryPlace` が `os.Link(tmp, lockPath)` を終えた後、`lock` は独立した hard link なので
tmp を消しても lock の中身は失われない。`O_EXCL` fallback 経路では tmp は lock と無関係。

### (iii) 同一プロセスの過去の試行の残骸は、そもそも別トークンで対象外

`acquire --wait` のループは反復ごとに `Acquire` を呼び、毎回新しい `mustToken()` で
probe と tmp を作る。ガードが持っているのは**今の** token 1 つなので、前の反復の残骸は
元からカバーしていない。

### 🚨 起票時の根拠は誤りだった（残しておく）

起票時は「probe / tmp の寿命はミリ秒、retention は 1 時間なので、1 時間より古い自分の
残骸は**原理的に**死んだプロセスのものしか存在しない」と書いた。**これは崩れる**:

- 削除は両方ともエラーを捨てている（`defer func() { _ = os.Remove(name) }()` /
  `_ = os.Remove(tmp)`）。`os.Remove` が失敗すれば残骸は残る
- **長寿命プロセスは実在する**: `with`（renew し続けるので数時間）と
  `acquire --wait 3h`（`--wait` に上限が無く、busy のたびに Acquire を回す）

つまり「ミリ秒で消える」は**成功パスの話**で、`_ =` で捨てた失敗パスを勘定していなかった。
生きているプロセスが 1 時間超の自分の残骸を持つ経路は実在する。それでも (i)(ii)(iii) から
**ガードを配線する価値はゼロ**。この誤りを消さずに残すのは、次に読んだ人が同じ経路を
見つけて「やはりガードが要る」に戻すのを防ぐため。

## このガードがマスクしている failure mode（外す前に列挙する）

`_claude/rules/list-masked-failure-modes-before-removing-guard.md` の手順。

- **本来の目的**（「進行中の自分の acquire を消さない」）: (i)(ii)(iii) から成立しない
- **副次的に守っているもの**: `scratchRetention` を**短くしたとき**の防御。
  `cleanup.go` の const のコメントは「**短くしないこと**（縮めるほど走行中の acquire を
  消す確率が上がる）」と明記しており、このガードはその指示を破った場合の 2 段目に当たる
- **外した後に誰が守るか**: `scratchRetention` の値そのものだけ = **単一障害点**になる

したがって対処は「消すだけ」では足りない。

## 推奨対応

**A（推奨）: ガードと引数を削除し、代わりに retention の下限を機械で固定する**

1. `scratchRetention` に下限の検査を置く（テストで `scratchRetention >= 10*time.Minute` を
   assert する等）。**これが 2 で失う防御の代替**
2. `Cleanup` の `selfToken` 引数とガードを削除、コメント（「自分の残骸は消さない」）も撤去
   — `_claude/rules/comment-no-restate-enforced.md`: 成立していない不変条件を残さない
3. `TestCleanupKeepsOwnScratch` を削除し、**失ったカバレッジ（テスト本体）を本 issue に貼る**
   （`_claude/rules/refuse-low-value-coverage.md`「テストを削除するときは失ったカバレッジを
   issue に起こす」）。検出可能性は 1 で作るので `検出できることを実証済み` に書ける

**B: production へ配線する** — `Acquire` は自分の token を持っているので渡せるが、
(i) により probe には届かず、(ii)(iii) により tmp に届いても無意味。採らない理由を
コード側にも残すこと（`_claude/rules/pending-issue-rationale-in-code.md`）。

🚨 **1 を先に入れてから 2・3 を入れること**（順序を逆にすると、その間だけ retention が
無防備になる）。1 を入れたら、`scratchRetention` を 1 分へ変える変異でその検査が
red になることを確認する。

## todolist

- [x] `scratchRetention` の下限検査を追加し、値を縮める変異で red を確認
- [x] `Cleanup` の `selfToken` 引数とガード・コメントを削除
- [x] `TestCleanupKeepsOwnScratch` を削除し、消した中身を本 issue に貼る
- [x] `make -C src/lockman test` / `make -C src/lockman lint`

## 実施結果 (2026-09-11)

### 1. 下限は「sweep に渡る値」を縛る (第 1 版のコンパイル時検査は敵対レビューで崩れた)

**第 1 版**は `const _ = uint(scratchRetention - minScratchRetention)` でコンパイル時に
固定した。359 の残タスクが保留していた「コンパイル時に置けるか」は**置けた**が、
**敵対レビューが 3 形の迂回を実測して崩した**。軸が「定数の値」にあったのが原因:

| 変異 | 第 1 版 | 現行 |
|---|---|---|
| **M1** `scratchRetention` だけ 1 分 | build rc=1 (検知) | test rc=1 `tmp の保持期間 1m0s が下限 10m0s を下回る` |
| **M2** probe だけ別の値 (1s) を `sweep` へ渡す | **緑で素通り** | test rc=1 (同上 + `removed=2 (期待 3)`) |
| **M7** `scratchRetention` と下限を一緒に下げる | **緑で素通り** | test rc=1 `minRetention=1m0s が defaultIOTimeout=10s の 10 倍未満` |
| **M4** probe の sweep 行を丸ごと削除 | **緑で素通り** | test rc=1 `removed=2 (期待 3)` |
| **M9** 下限チェック自体を削除 | (第 1 版には該当なし) | test rc=1 `TestSweepRefusesRetentionBelowFloor` |

M1 で出るエラー `constant -540000000000 overflows uint` が**どの定数が何に違反したかを
名乗らない**ため、踏んだ人の自然な反応が「下限も一緒に下げる」(= M7) になる、という指摘が
特に効いた。M4 は「probe の sweep はテストが 1 本も見ていない」という**元からの穴**。

**現行版** (`adversarial-review-own-safeguards.md` §8「構文でなく効果へ軸を移す」):

- `sweep` のロジックを `(*Locker).sweepDir(sub, retention, now, *CleanupResult)` へ切り出し、
  **retention が下限を割っていたら何も消さずにエラーを積んで返す** (fail-closed)。
  これで「別の定数を作って渡す」も「呼び出し側で式にする」も、渡った値で捕まる
- 下限そのものを下げる変異は下限チェックからは原理的に見えないので、
  `TestMinRetentionIsFarAboveDefaultIOTimeout` で **`minRetention >= 10 × defaultIOTimeout`** を
  別に固定した
- `probe/` の fixture を `TestCleanupRemovesOldScratchOnly` に足した (M4 を殺す)

**下限を `cleanupInterval` (レート制限) に相対させなかった理由**: 両者は無関係な量で、
「掃除の頻度」を変えると「走行中の acquire を守る余裕」が連動して動くのは誤った結合になる。

### 2. 変異検証の記録

上表のとおり 5 変異すべてが red。各変異は `diff` で 1 箇所だけが変わっていることを目視し、
`go build` の成否をテスト実行と分けて見た。

🚨 **M9 の初回は「ビルド不能」だった** — 下限チェックを消すと `fmt` が未使用になり
コンパイルが通らない。`mutation-verify-new-tests.md` の第 3 の結果 (red でも green でもない)
として扱い、`fmt` の import ごと落とす形で当て直して red を確認した。

### 3. ガードの撤去

- `Cleanup(force bool, selfToken string)` → `Cleanup(force bool)`
- sweep 内の `selfToken` ガードとコメント「自分の残骸は消さない」を削除
  (`comment-no-restate-enforced.md`: 成立していない不変条件を残さない)
- 呼び出し側の更新: production 1 件 (`main.go:211`) + テスト 7 件
- **勘定の裏取りは grep 以外でも取った**。`mutation-verify-new-tests.md`「2 者が
  独立に数えても数え方が同じなら独立ではない」に当たるため、シグネチャを変えて
  `go build ./...` / `go vet ./...` が通ること (= 引数 2 個の呼び出しが 1 件も
  残っていないこと) をコンパイラに数えさせた。dotfiles-4b が独立に数えた結果
  (production 1 + テスト 8、実トークンは 1 件) とも一致した

### 4. 削除したテストの本体 (失ったカバレッジの記録)

`refuse-low-value-coverage.md`「テストを削除するときは失ったカバレッジを issue に起こす」。

```go
func TestCleanupKeepsOwnScratch(t *testing.T) {
	l := newTestLocker(t)
	if err := l.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	token := mustToken()
	mine := filepath.Join(l.metaDir, tmpDirName, token+".json")
	touchOld(t, mine, 2*time.Hour)
	l.Cleanup(true, token)
	if _, err := os.Stat(mine); err != nil {
		t.Fatalf("自分の残骸を消した: %v", err)
	}
}
```

同時に直前のコメント「走行中の他者を巻き込まない: 自分の token の残骸には触らない。」も削除した。

**削除の理由**: このテストは production から到達不能な引数 (`selfToken != ""`) を
自分で作って観測しており、production の挙動を 1 mm も守っていなかった
([315](done/315-test-unused-includes-tests-so-production-unreachable-code-stays-green.md) /
[317](done/317-test-termsafe-regression-test-observes-production-unreachable-surfaces.md) と同型)。

**失ったカバレッジ — 2 つに分けて書く** (🚨 起票直後に書いた第 1 版は**実測で崩れた**。
敵対レビューの指摘を反映した版がこれ):

| 区分 | 内容 | 検出可能性 |
|---|---|---|
| **削除で失った分** | production の挙動は**ゼロ** (`selfToken != ""` は到達不能)。ただし厳密には「**ガードが存在すること自体の pin**」を失った — 削除前はガードを消すと `TestCleanupKeepsOwnScratch` が red になった | 該当なし (守る対象が production に無いので、pin を失っても失うものが無い) |
| **元から穴だった分** (削除の責任ではない) | 「走行中の acquire の scratch を掃除が消さない」という本来の不変条件 | **検出できることを実証済み** (上の変異表 M1 / M2 / M4 / M7 / M9) |

🚨 **第 1 版は「元からテストは 1 本も無かった」と書いたが、これは誤り**。
`TestCleanupRemovesOldScratchOnly` の fixture `touchOld(t, freshTmp, time.Minute)` が
**tmp の保持期間を約 1 分に偶然 pin していた** (それより短くすると `freshTmp` が消えて
`removed` が合わなくなる)。したがって今回動かした下限は **0 → 10 分ではなく約 1 分 → 10 分**。
結論の向き (正味で増えている) は変わらないが、**増分は第 1 版の主張の 1/600 のスケール**。
しかも「1 分」は宣言された下限ではなく fixture の副作用なので、`probe/` 側にはそれすら
無かった (M4 が緑で通った理由)。

## 進捗

- 2026-09-11: 起票。到達不能性を機械照合。反証レビューで起票時の根拠（寿命 vs retention）が
  崩れ、(i) probe の命名が独立乱数 / (ii) `os.Link` 後の tmp は不要 / (iii) 過去の試行は
  別トークン という 3 点へ根拠を差し替えた（未着手）
- 2026-09-11: 推奨対応 A を実施 (上の「実施結果」)。1 → 2・3 の順序を守り、下限検査を
  先に入れてから撤去した (第 1 版 = コンパイル時の定数検査、`ff8ed1c4`)
- 2026-09-11: **敵対的レビュー 1 周目**。第 1 版の下限検査が 3 形 (M2 / M4 / M7) で
  緑のまま迂回されることを実測で示され、**軸を「定数の値」から「sweep に渡る値」へ移した**。
  併せて「失ったカバレッジ = なし」「元からテストは 1 本も無かった」という本文の主張、
  README が名指ししたテスト、const 群コメントの射程縮小の 4 件も崩れたので直した

## 残タスク

- **敵対的レビュー 2 周目が必要**。`adversarial-review-own-safeguards.md` §7
  「指摘を直した差分にもう 1 周回してから閉じる」。1 周目の指摘で**新設した**もの
  (`sweepDir` の切り出し・fail-closed の下限・`TestMinRetentionIsFarAboveDefaultIOTimeout`・
  probe fixture) はまだ誰にも攻められていない
- 決着済み: 下限はコンパイル時ではなく `sweepDir` の実行時に置いた (上の「実施結果」1)。
  コンパイル時にも**置けた**が、定数を縛る形は迂回されるため採らなかった

## 却下した指摘 (敵対レビュー 1 周目。理由を残す — 次の監査が再生成しないため)

- **`--io-timeout` に上限の検証が無く、5h を渡すと `minRetention` の根拠が崩れる (P3)** —
  指摘自体は実在する (実測で `--io-timeout 5h` が rc=0 で受理される)。ただし**本 commit が
  持ち込んだ穴ではなく**、`--on-lost` の無検証と同じ族なので
  [359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) の「軽微だが実在する」節へ
  移し、356 / 357 の実装時にまとめて直す。`cleanup.go` のコメントには「既定の I/O の上限」と
  書き、成立条件が既定値に限ることを明記した
- **`graveyardRetention` / `cleanupInterval` に同型の危険はないか (P3)** — `graveyardRetention` は
  現行版では `sweepDir` を通るので下限に守られる。`cleanupInterval` は sweep に渡らないので
  対象外だが、`TestCleanupIsRateLimited` が pin していることを実測で確認した
  (`cleanupInterval = time.Nanosecond` で red)

## 関連

- [issue 315](done/315-test-unused-includes-tests-so-production-unreachable-code-stays-green.md) / [issue 317](done/317-test-termsafe-regression-test-observes-production-unreachable-surfaces.md) — 同型（production 到達不能な面をテストが観測）
- [issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) — この issue の出典（監査記録）
