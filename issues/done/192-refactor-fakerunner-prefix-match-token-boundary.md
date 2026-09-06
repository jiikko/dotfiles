# 192 refactor: `fakeRunner` の応答選択がトークン境界を見ておらず、将来の fixture で誤マッチする

起票日: 2026-09-03
重要度: **P3**
関連: [issues/done/168](168-bug-doctor-simdevice-xctestdevices-set.md) (敵対レビュー 1 周目の記録)

## 対象

`src/doctor/disk/scan_test.go` の `fakeRunner.run` (応答キーの選択)

## 何が起きるか

`fakeRunner` は「コマンド行に対する **最長の `strings.HasPrefix` 一致**」で応答を選ぶ。
トークン境界を見ていないので、**あるキーの全体が別のコマンド行の literal な prefix になっている**と、
短い方のキーが長い方の呼び出しを横取りする。

現状は誤マッチしない (2026-09-03 の敵対レビューが確認):

- `xcrun simctl --set <dir> list devices -j` に渡す 3 つのパス
  (`~/Library/Developer/XCTestDevices` / `XCPGDevices` / `.../Previews/Simulator Devices`) は
  互いに prefix 関係に無い
- 他のキー (`pgrep -x` / `brew info` / `brew --prefix` / `brew cleanup --dry-run`) も同様

**将来 `.../FooDevices` と `.../FooDevicesExtra` のような prefix 関係にある fixture を足すと壊れる。**
壊れ方は「テストが別の応答を拾って green のまま意味を失う」なので、気づきにくい。

## 対応案

- キーの一致を「トークン境界まで見る」形にする (キーの直後が空白か行末であることを要求する)
- または fixture を `[]string` の argv で持ち、`slices.Equal` / prefix を argv 単位で比べる

## 受け入れ条件

- [x] prefix 関係にある 2 つのキーを登録したテストで、長い方の呼び出しが長い方の応答を拾う
- [x] 変異検証: トークン境界の判定を外すと誤マッチが再現する

## 事前検証 → 対応 (2026-09-03)

### trigger を待たずに着手した判断 (ユーザー判断 2026-09-03)

[`verify-design-intent-before-refactor.md`](../../_claude/rules/verify-design-intent-before-refactor.md) の
「実需要 trigger 待ち」が想定しているのは**分解・抽象化のような、後で不要と分かると剥がすのが高くつく
変更**。今回は判定を 1 つ厳しくするだけで、剥がすコストがほぼ無い。さらに壊れ方が
「green のまま意味を失う」側なので、待つ間の損の方が大きい。

### 着手前に再現した — ただし issue の記述とは壊れ方が違った

issue 本文は「**短い方のキーが長い方の呼び出しを横取りする**」と書いているが、
**最長一致があるので両方登録されていれば起きない** (実測: 両方登録して長い方を呼ぶと `LONG` が返る)。

実際に壊れるのは「**応答を登録していない呼び出しが、別のキーの literal な prefix に吸われる**」形:

| 登録したキー | 呼んだコマンド | 修正前 | 修正後 |
|---|---|---|---|
| `--set /d/FooDevices` のみ | `--set /d/FooDevicesExtra ...` | `SHORT` (rc=0) | `unexpected` (rc=1) |

テストが「このコマンドは呼ばれないはず = `unexpected` になるはず」と期待していても別の応答が返るので、
**green のまま意味を失う**。issue の懸念そのものは正しく、経路の説明だけが違っていた。

### 実装

`fakeRunner.run` の候補選別に「キーの直後が行末か空白であること」を足した (最長一致は据え置き)。
パスの途中で切れた一致が候補から外れるので、登録していない呼び出しは `unexpected` に落ちる。

### 検証

`TestFakeRunnerMatchesOnTokenBoundary` を追加。登録したキーちょうどなら拾う (assert が
「常に unexpected」で通らないことの確認) / 途中で切れた一致は拾わない / prefix 関係の 2 つを
両方登録したら双方が自分の応答を拾う、の 3 点。

変異検証: トークン境界の判定を外すと `未登録の呼び出しが別のキーの応答を拾った: out="SHORT" rc=0` で red。
🚨 **同じ変異を当てても既存テストは全部 green のまま**だった (新テストを除いて実行して確認)。
これは issue の「現状は誤マッチしない」を裏付けている = **この修正は今のテストを直したのではなく、
将来の fixture のために境界を作ったもの**。

`make test` rc=0。
