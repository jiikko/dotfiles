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

### 2.5 敵対レビュー 2 周目 — 1 周目の**修正そのもの**が P1 で崩れた

`adversarial-review-own-safeguards.md` §7 (指摘を直した差分にもう 1 周) の実施結果。
**1 周目で新設したものが攻められ、P1 が 1 件出た。**

#### P1: M7 を閉じるために足した検査が、元の軸 (定数の比) に戻っていた

`TestMinRetentionIsFarAboveDefaultIOTimeout` は `minRetention >= 10 * defaultIOTimeout` を
見ていた。これは**編集可能な 2 定数の比**なので、**両方下げれば緑のまま通る**。
レビュワーの実測 (M7p: `minRetention` 10m→1m / `scratchRetention` 1h→2m /
`defaultIOTimeout` 10s→6s) は 3 回とも `ok lockman`。実効の床が 10 分→1 分に落ちても
テストが 1 本も落ちない。

さらに悪いことに、**比で書いた版の失敗メッセージが迂回を教える**:
`minRetention=1m0s が defaultIOTimeout=10s の 10 倍未満` は両方のオペランドと必要な比を
名指しするので、踏んだ人に「もう一方を下げれば緑」と読ませる。1 周目が指摘した
`constant ... overflows uint` が「どの定数が何に違反したか名乗らない」ことと**同じ構造**で、
親切なぶん誘導力が強い。

**直した形**: 下限の**絶対値をリテラルで pin** する (`TestMinRetentionFloorIsPinned`)。
リテラルなら下げるにはテストを書き換えるしかなく、それは意図的な行為として diff に出る。
`defaultIOTimeout` との関係は「下限を見直す合図」として別 assert に残したが、
**それ単独は pin の代わりにならない**ことをテストのコメントに明記した。

#### P2: fail-closed にしたのに production では完全に無音だった

レビュワーの E2E: 下限を割ったビルドで `lockman cleanup` を打つと
**rc=0 / stdout 0 バイト / stderr 0 バイト**、しかも `.cleanup_at` は打刻済み。
`res.Errors` は `main.go` の `o.verbose` 分岐でしか出ず、`stampCleanup()` は sweep の後に
無条件で走るので、**拒否したのにレート制限が進んで以後 10 分は skip** される。
`verify-execution-not-just-exit-code.md`「沈黙 = 成功になっていないか」に直撃。
1 周目版ではこれがコンパイルエラーだったので、**2 周目の版が無音化を持ち込んだ**形。

**直した形** (どちらも `CleanupResult.Refused` を新設して実現):
- 拒否したときは `stampCleanup()` を**打たない** (毎回エラーを出し直す)
- 拒否は `o.verbose` に関係なく stderr へ出す

E2E で再確認 (使い捨て sandbox、stdout / stderr / rc を分離):

```
rc=0
stdout: []
stderr: [lockman: cleanup: 拒否した: [tmp の保持期間 5m0s が下限 10m0s を下回る...]]
掃除後: tmp/old.json と probe/old が残存 (graveyard/old のみ削除 = 下限を割っていない)
.cleanup_at: No such file or directory   ← 打刻されていない
```

**rc は 0 のまま**にした。`dispatch` の戻り値は無名 `int` で defer から書き換えられず、
この拒否は**壊れたビルドでしか起きない**ので、署名を変える価値より risk が大きい。
stderr と非打刻で観測できるので沈黙ではない。

#### 2 周目の変異検証 (3 本。うち 1 本が緑で、塞いだ)

| 変異 | 結果 |
|---|---|
| **M7p** 3 定数を一緒に下げる | test rc=1 `TestMinRetentionFloorIsPinned` |
| **M10** 打刻の条件を反転 (`!res.Refused` → `res.Refused`) | test rc=1 (`TestCleanupIsRateLimited` / `TestCleanupStampsWhenNotRefused` / `TestCleanupRunsOnlyForMutatingCommands` の 3 本) |
| **M11** `res.Refused = true` を削除 | 🚨 **初回は緑** — `TestSweepRefusesRetentionBelowFloor` が「消さなかったこと」しか見ておらず、フラグ自体を見ていなかった。`Refused` は打刻の抑止と stderr 出力を**両方**ゲートしているのに無検査だった。assert を足して再実行し red を確認 |

M11 は「観測している量が、壊れたときに動かない」形。**2 周目の修正を入れた直後に
その修正自身へ変異を当てて初めて出た。**

#### 却下した 2 周目の指摘

- **`sweepDir` は `retention` しか見ておらず `now` が無検査 (P2)** — 指摘は構造的には正しく、
  レビュワーは `now` を 2 時間先に振って「下限を割らずに新しい残骸を消す」を実測した。
  ただし **production ではその入力を作る経路が無い**: `now` も `info.ModTime()` も同じ
  `serverNow()` (サーバ側の打刻) 由来。レビュワー自身も到達可能性は未確認としている。
  **推測に基づく防御コードは足さない**方針に従い、`sweepDir` のコメントに理由を残した
  (`pending-issue-rationale-in-code.md`)。`Renew` の `clockSkewTolerance` は打刻が
  クライアント側へ落ちる場合の検算で、掃除は正しさに関与しないため同じ検算は要らない
- **テスト名が `defaultIOTimeout` を指すが実際の窓は `l.timeout` (P3)** — `cleanup.go` の
  コメントで既に開示済み (`--io-timeout` に上限検証が無いこと)。テスト名は
  `TestMinRetentionFloorIsPinned` に変えたので乖離は解消

### 2.6 敵対レビュー 3 周目 — **2 周目の変異検証が偽物だった**

#### P1: M10 は「production の機構を戻す」形の変異ではなかった

2 周目は「M10 (`!res.Refused` → `res.Refused` の反転) が red」を、**「拒否時は打刻しない」が
守られている証拠**として記録した。これは誤り。反転は**正常系を壊す**ので、既存の
`TestCleanupIsRateLimited` / `TestCleanupRunsOnlyForMutatingCommands` が元から拾う。

正しい変異は **M12 = 機構を戻す** (`if !res.Refused { stamp }` → 無条件 `stamp`)。実測:

```
M12: go build rc=0 / go test rc=0 / FAIL 0 件    ← 緑で素通り
```

`mutation-verify-new-tests.md`「変異は production の機構を戻す形にする」「その変異は
実際に起こりうる退行の形か」に正面から抵触していた。2 周目で新設した
`TestCleanupStampsWhenNotRefused` も検出力の増分がゼロ (既存 2 本と完全に重複)。

🚨 **害は runtime のバグではなく、偽の検証記録**だった。commit message の表と
`cleanup_test.go` のコメント「そちらは変異で確認した」が、次に読む人に
「決着済み」と読ませる (`pending-issue-rationale-in-code.md` の逆作用)。

#### P2: 観測性を「到達不能な枝」にだけ足していた

2 周目は下限違反 (= 壊れたビルドでしか起きない) にだけ非打刻と警告を付けた。
一方 **実際に起きる失敗**である `ReadDir` / `os.Remove` の権限エラーは `Refused` を
立てないので、従来どおり**無音 + 打刻**のままだった。レビュワーの E2E:
`rc=0 / stdout 0B / stderr 0B / .cleanup_at 打刻済み`。
commit message の理由づけ「黙って 10 分止まるのを防ぐ」がこの枝に逐語で当てはまるのに、
適用先が逆になっていた。

#### 直した形 — `Refused` を廃し、ゲートを `len(res.Errors) == 0` にした

P1 と P2 を同時に閉じ、しかも**機構が production から到達可能になる**ので
テストで守れるようになった (2 周目版は到達不能だったので守れなかった):

- `Cleanup` の末尾を `if len(res.Errors) == 0 { l.stampCleanup() }` にした。
  下限違反だけでなく**権限ドリフト / stale mount** でも打刻しない
- `main.go` は `if len(res.Errors) > 0 || o.verbose` で警告する。
  **件数も一緒に出す** — 部分的に進んだのか何も進んでいないのかは失敗時こそ知りたい
  (2 周目版は排他分岐で、拒否時に `removed` が消えていた。3 周目 P3-c)
- `CleanupResult.Refused` と `json:"refused,omitempty"` を廃止
  (出力経路が無く「出るように見えて出ない」タグだった。3 周目 P3-b)
- テストを**到達可能な失敗** (chmod 0500 による権限エラー) で書き直した:
  `TestCleanupDoesNotStampWhenSweepFails` / `TestCleanupStampsWhenClean`

#### 3 周目の変異検証 (4 本。すべて red)

| 変異 | 結果 |
|---|---|
| **M12** 機構を戻す (打刻を無条件に) | test rc=1 `TestCleanupDoesNotStampWhenSweepFails` ← 2 周目版では緑だった |
| **M13** 逆向き (常に打刻しない) | test rc=1 (`TestCleanupIsRateLimited` / `TestCleanupStampsWhenClean` / `TestCleanupRunsOnlyForMutatingCommands`) |
| **M14** 下限チェック自体を削除 | test rc=1 `TestSweepRefusesRetentionBelowFloor` |
| **M7p** 3 定数を一緒に下げる | test rc=1 `TestMinRetentionFloorIsPinned` |

E2E (使い捨て sandbox、正規ビルド、stdout / stderr / rc を分離):

```
1 回目 (tmp を chmod 0500):
  rc=0  stdout: []
  stderr: lockman: cleanup: removed=0 skipped=false errors=[remove .../tmp/old.json: permission denied]
  .lockman/ の中身: graveyard probe tmp        ← .cleanup_at が無い (打刻していない)
2 回目: 同じ警告が再び出る                      ← レート制限が進んでいない
権限を戻した 3 回目:
  rc=0  stderr: []   tmp/ は空   .cleanup_at あり
```

🚨 **この E2E は 1 回目に rc=126 を出して失敗した**。原因は実装ではなく
**ハーネス** — 3 周目レビュワーが scratchpad に作った変異用ディレクトリと
`go build -o` の出力先が同名で、ディレクトリを実行していた。
`verify-interactive-prompt-with-pty-driver.md`「確認が失敗したら実装を疑う前に
ハーネスを疑う」の適用例。

#### 却下した 3 周目の指摘

- **リテラル pin と比の assert、効いているのは比のほう (P3-a)** — 指摘は正しい
  (リテラルが単独で効くのは `minRetention ∈ [100s, 600s)` だけで、危険な 100s 未満は
  比が押さえている)。ただし**両方とも同じテスト関数の中で走る**ので検出力は落ちていない。
  誤っていたのは「比は見直しの合図で、単独では pin にならない」という**コメントの側**なので、
  実測どおり「どちらも本体で、帯が違う」へ書き直した。4 行 (production 3 + テスト 1) の
  迂回が通ることも、隠さずコメントに書いた — 上げているのは敷居であって不可能性ではない
- **`cleanup.go` の「status --json にだけ出す」が実装と乖離 (ぼやき)** — 実在する乖離
  (`status` は `Cleanup` を呼ばない)。触っているコードなので同じ commit で直した
  (`claude-md-maintenance.md`「触ったら直す」)

### 2.7 敵対レビュー 4 周目 — 3 周目の拡大が**退行を作っていた**

#### P2-1 (実害): 良性の ENOENT を失敗に数えていた — 3 周目が持ち込んだ退行

`os.Remove` の失敗を無条件に `res.Errors` へ積んでいたため、**並行する acquire の掃除どうしが
同じ残骸を取り合う正常系**で失敗が記録されていた。`ReadDir` 側には `os.IsNotExist` の
フィルタが在るのに `os.Remove` 側に無い、という非対称が直接の原因。

自分で A-B を採り直した (同一 dir へ 8 並行 acquire × 5 試行。`--ttl` の下限 30s を
踏んで 1 度ハーネスが空振りし、両腕が同じ結果を返す「判別しない観測」を作ったので、
fixture の作成失敗を検出する形に直してから測った):

| 版 | 打刻 | stderr | ENOENT |
|---|---|---|---|
| **修正後** (ENOENT は良性) | 5/5 あり | **0 B** × 5 | 0 |
| **3 周目の状態** (ENOENT も失敗) | 4/5 あり・**1/5 消失** | 695〜1755 B | 3〜8 件 |

実害は 2 つ。**レート制限が飛んで毎回フル sweep** になることと、より重要な
**観測チャネルの偽陽性** — 3 周目が作った「stderr の警告 = 掃除が止まっている」という
シグナルが、まったく正常な並行掃除で鳴る。`zshlib/_av1ify_lock.zsh` は
`lockman acquire ... >/dev/null` と **stdout しか落としていない**ので利用者の端末に出る。

`e.Info()` の失敗 (エラーを積まず `continue` していた) も同じ基準に揃えた。

#### P1-1: `t.Skip` が「3 周目が直したはずの退行」を緑に畳んでいた

`TestCleanupDoesNotStampWhenSweepFails` は `len(res.Errors) == 0` のとき `t.Skip` していた。
そのため **`os.Remove` のエラー記録を消す変異** (= 3 周目が直した「無音」へ戻す変異) が
`--- SKIP` になり `make test` は rc=0。CI は `-v` なしなので skip はログからも読めない。
`adversarial-review-own-safeguards.md` §2「判定不能を緑に畳まない」に抵触。
→ **前提が作れなかったら `t.Fatal`** に変えた (環境が作れないならそのテストは守りとして
成立していない)。

#### P1-2: 3 周目が production で変えた 3 点のうち 2 点が完全に無検査だった

- `ReadDir` が落ちる枝 (コメントが「本命」と書いていた枝) のテストが 0 件 →
  `TestCleanupDoesNotStampWhenReadDirFails` (`chmod 0000`) を新設。
  `chmod 0500` の既存テストとは**実装の別分岐**なので片方では守れない
- `main.go` の警告分岐は**丸ごと消しても `o.verbose` に戻しても緑** →
  `TestDispatchWarnsOnCleanupFailureWithoutVerbose` を新設 (`os.Stderr` を差し替えて捕捉)

#### P2-3 / P2-4 (自分の記録の誤り。訂正した)

- 3 周目の E2E「2 回目も警告が出る = レート制限が進んでいない」は**証拠になっていなかった**。
  `lockman cleanup` は `force=true` で `cleanupDue()` を丸ごと飛ばすので、打刻の有無に
  関わらず 2 回目も sweep する = **機構の有無で結果が変わらない観測**
  (`verify-execution-not-just-exit-code.md`)
- `cleanup.go` のコメント「コストは readdir 1 回ぶん」も誤り。失敗が持続するあいだ
  レート制限が丸ごと死ぬ。実測 (ローカル APFS / graveyard 2000 件): **12.2 → 17.5 ms/acquire
  = +43%**。SMB では未実測で、trigger は「SMB 共有で acquire が遅い報告が出たら同じ A-B を採る」。
  コメントを実測値ごと書き換えた

#### 4 周目の変異検証

| 変異 | 結果 |
|---|---|
| **M16** `ReadDir` のエラー記録を削除 | rc=1 `TestCleanupDoesNotStampWhenReadDirFails` |
| **M19** `os.Remove` のエラー記録を削除 | rc=1 (`…WhenRemoveFails` / `TestDispatchWarns…`) ← `t.Skip` 時代は緑 |
| **M17** 警告を `o.verbose` 限定へ戻す | rc=1 `TestDispatchWarns…` |
| **M21** 警告から件数を落とす | rc=1 `TestDispatchWarns…` |
| **M18** 警告を丸ごと削除 | **`<変異がビルド不能>`** (`res` が未使用になる) = 第 3 の結果。同じ面は M17 / M21 が覆う |
| **M20** ENOENT フィルタを外す (今回の修正を戻す) | 🚨 **緑**。下記 |

🚨 **M20 が緑 = ENOENT フィルタには単体テストの回帰カバレッジが無い**。
ReadDir と Remove のあいだで他者が消す状態を、seam 無しで決定論的に作れないため
(ReadDir 前に消すと一覧が空になり、その経路を 1 度も通らない = vacuous。実際に 1 本
書きかけて捨てた)。**検出可能性: 上の A-B 実験で実証済み (単体テストは無い)**。
再評価の trigger: `sweepDir` に seam を入れる変更が来たとき、この経路のテストを足す。

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
- 2026-09-12: **敵対的レビュー 4 周目**。3 周目のゲート拡大が**退行を作っていた** —
  良性の ENOENT (並行する掃除どうしの取り合い) を失敗に数え、正常系で警告が鳴り
  レート制限が飛んでいた。A-B を自分で採り直して確認 (修正後 stderr 0B × 5 /
  3 周目版 695〜1755B × 5)。`t.Skip` が退行を緑に畳んでいた件と、`ReadDir` の枝・
  `main.go` の警告分岐が無検査だった件も直した (上の「2.7」)
- 2026-09-12: **敵対的レビュー 3 周目**。2 周目の変異検証が偽物だった (M10 は正常系を
  壊す変異で、既存テストが元から拾う範囲。機構を戻す M12 は緑で素通り) / 観測性を
  到達不能な枝にだけ足していた、の 2 件が P1・P2 として出た。`Refused` を廃して
  ゲートを `len(res.Errors) == 0` に広げ、**到達可能な失敗 (権限ドリフト) で**
  テストを書き直した (上の「2.6」)
- 2026-09-12: **敵対的レビュー 2 周目**。1 周目の修正で足した
  `TestMinRetentionIsFarAboveDefaultIOTimeout` が定数の比で書かれており、3 定数を
  一緒に下げると緑で通る (P1) / fail-closed が production で完全に無音 (P2) が出た。
  下限をリテラルで pin し、`Refused` を新設して打刻抑止と stderr 出力を付けた。
  変異 M11 が緑で通ったので `Refused` の assert も足した (上の「2.5」)
- 2026-09-11: **敵対的レビュー 1 周目**。第 1 版の下限検査が 3 形 (M2 / M4 / M7) で
  緑のまま迂回されることを実測で示され、**軸を「定数の値」から「sweep に渡る値」へ移した**。
  併せて「失ったカバレッジ = なし」「元からテストは 1 本も無かった」という本文の主張、
  README が名指ししたテスト、const 群コメントの射程縮小の 4 件も崩れたので直した

## 残タスク

- **敵対的レビュー 5 周目の要否がユーザー判断待ち**。4 周目の修正も判定ロジックを
  触っている (`os.IsNotExist` フィルタの新設) ので §7 の打ち切り条件 (a) は満たさない。
  一方で 1〜4 周目はいずれも P1 を出しており、止める根拠も無い。**費用の判断はユーザー**
- **ENOENT フィルタの単体テストが無い** (上の M20)。seam 無しでは決定論的に作れない。
  A-B 実験で検出可能性は実証済み
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
