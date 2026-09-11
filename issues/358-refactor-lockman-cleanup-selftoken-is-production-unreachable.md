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

### 1. 下限検査は**コンパイル時**に置いた (テストではない)

359 の残タスクが「コンパイル時に置けるかは実装時に決める」と保留していた点の決着。
`cleanup.go` に次を置いた:

```go
// minScratchRetention は scratchRetention の下限。tmp/probe の実寿命はミリ秒だが、
// 保持期間は「I/O が返らないまま進行中の他者」を巻き込まないための余裕なので、
// 1 回の I/O の上限 (defaultIOTimeout) より桁で長く取る。
const minScratchRetention = 10 * time.Minute

// 下限をコンパイル時に強制する。scratchRetention を下限より短くすると差が負になり、
// uint への変換が「constant ... overflows uint」で落ちる。
const _ = uint(scratchRetention - minScratchRetention)
```

配列長を使う形 (`var _ [scratchRetention - minScratchRetention]struct{}`) も試して
両方が機能したが、`uint` 変換のほうが意図が読めるので採用した。

**下限を `cleanupInterval` (レート制限) に相対させなかった理由**: 両者は無関係な量で、
「掃除の頻度」を変えると「走行中の acquire を守る余裕」が連動して動くのは誤った結合になる。

### 2. 変異検証 — red ではなく「ビルド不能」で検出される

コンパイル時の検査なので、`mutation-verify-new-tests.md` の言う第 3 の結果
(`<変異がビルド不能>`) が**この検査にとっての正しい検出の形**。判定は
「その検査を**名指しする**エラーで落ちるか」で行った。

| 実行 | `go build` rc | 出力 |
|---|---|---|
| baseline (`scratchRetention = time.Hour`) | 0 | — (`go test` も `ok lockman 1.874s`) |
| 変異 (`scratchRetention = time.Minute`) | **1** | `./cleanup.go:24:16: constant -540000000000 overflows uint` |
| **対照**: 同じ変異 + ガード行を削除 | **0** | — |

3 行目が要点。`verify-execution-not-just-exit-code.md`「その機構を外したら観測結果は
変わるか」に yes と答えられる = **検知していたのはこのガードである**ことが確定した
(変異の diff は 1 行だけであることを `diff` で目視確認済み)。

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

**失ったカバレッジ — 2 つに分けて書く**:

| 区分 | 内容 | 検出可能性 |
|---|---|---|
| **削除で失った分** | なし。`selfToken != ""` の分岐は production から到達しないので、守っていた production の挙動はゼロ | — |
| **元から穴だった分** (削除の責任ではない) | 「走行中の acquire の scratch を掃除が消さない」という**本来の不変条件**。これは元々 `scratchRetention` の値だけが守っており、テストは 1 本も無かった | **検出できることを実証済み** — 上の 1 で入れたコンパイル時の検査が、値を縮める変異を名指しで落とすことを実測した (上表) |

つまりこの削除は保護を減らしていない。**元からあった単一障害点 (`scratchRetention` の値)
に機械の検査を付けたぶん、正味では増えている**。

## 進捗

- 2026-09-11: 起票。到達不能性を機械照合。反証レビューで起票時の根拠（寿命 vs retention）が
  崩れ、(i) probe の命名が独立乱数 / (ii) `os.Link` 後の tmp は不要 / (iii) 過去の試行は
  別トークン という 3 点へ根拠を差し替えた（未着手）
- 2026-09-11: 推奨対応 A を実施 (上の「実施結果」)。1 → 2・3 の順序を守り、下限検査を
  先に入れてから撤去した。`make -C src/lockman lint` = `0 issues.` (rc=0) /
  `make -C src/lockman test` = `ok lockman 2.905s` (rc=0、stdout / stderr を分けて確認し
  `✗` / `FAIL` はいずれも 0 件)

## 残タスク

- **敵対的レビュー未実施**。359 が「358 の実装差分には改めて敵対的レビューが要る」と
  要求しており、かつ本変更は「自作の検査の新設」(`adversarial-review-own-safeguards.md`) と
  「ガードの撤去」(`list-masked-failure-modes-before-removing-guard.md`) の**両方**に
  該当する。commit 後に観点を分けた read-only レビューを 1 体 (opus 級) 通す
- 決着済み: 下限検査はコンパイル時に置けた (上の「実施結果」1)

## 関連

- [issue 315](done/315-test-unused-includes-tests-so-production-unreachable-code-stays-green.md) / [issue 317](done/317-test-termsafe-regression-test-observes-production-unreachable-surfaces.md) — 同型（production 到達不能な面をテストが観測）
- [issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) — この issue の出典（監査記録）
