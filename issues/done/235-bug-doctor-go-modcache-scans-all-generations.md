# doctor: `go-modcache` は全世代の GOPATH を走査するが、`go clean -modcache` は 1 つしか消さない

起票日: 2026-09-04
種別: bug (カタログの作法とガードの食い違い)
優先度: **P2** (毎回必ず「未完了」になり、いちばん消したい古い世代がツールから永久に消せない)
出典: audit (broken-code / design) 2026-09-04 / forge-Standard。main agent が実機で裏取り済み

## 該当

`src/doctor/disk/catalog.go` の `go-modcache` エントリ:

- `Paths: []string{"~/go/*/pkg/mod", "~/go/pkg/mod"}` / `Detail: "…全世代の GOPATH を見る"`
- `DeleteVia: "cli:go clean -modcache"`

## 症状

走査は `~/go/*/pkg/mod` を glob するので**複数世代**を候補に入れるが、`go clean -modcache` が
消すのは `GOMODCACHE` (= 今の `go` が指す 1 つ) だけ。したがって削除後の再走査には他の世代が残り、
`verifyEntry` の `len(after.Items) > 0` が成立して**常に `OutcomeIncomplete`**
「時間をおいて再スキャンしてください」になる。時間をおいても消えない。

実測 (2026-09-04, この開発機):

```
$ ls -d ~/go/*/pkg/mod ~/go/pkg/mod
/Users/koji/go/1.23.12/pkg/mod  /Users/koji/go/1.23.4/pkg/mod  /Users/koji/go/1.26.0/pkg/mod
$ go env GOMODCACHE
/Users/koji/go/1.26.0/pkg/mod
```

= 3 世代のうち 1 世代しか消せない。**残る 2 世代 (使っていない古い Go の module キャッシュ) が
いちばん消したい対象**なのに、`deleteVia: cli:` なので rm 経路には乗らない。

## 発火条件

`~/go/<version>/pkg/mod` が 2 つ以上ある環境 (asdf / anyenv / goenv 等で Go を複数入れている)。
1 世代だけの環境では発火しない。

## silent か

**silent ではない** (毎回「未完了」と出る) が、原因が「カタログの走査範囲と削除手段のスコープ違い」
であることは画面から読めない。ユーザーは何度も再スキャンする。

## 反証の試み

- 「`GOFLAGS` や `GOMODCACHE` を差し替えて世代ごとに `go clean` を回せば」→ できるが、
  それは `cli:` 1 本という現在の作法を超える (エントリを分ける設計判断が要る)
- 「古い世代を rm してよいのか」→ module キャッシュは read-only で作られるため rm には
  `chmod -R` 相当の強制が要る (`Detail` が既にそう書いている)。だから `cli:` を選んだ経緯があり、
  素朴に `DeleteVia: rm` へ変えると別の失敗に化ける

## 最小の修正方向 (どちらかを選ぶ判断が要る)

1. 走査を `GOMODCACHE` の 1 つに絞る (`Detail` の「全世代を見る」を撤回)。
   → 古い世代は候補に出なくなる = 見えなくなる。**false green になるので単独では採らない**
2. `<id>` 相当の仕組みで世代ごとに `GOMODCACHE=<path> go clean -modcache` を回す。
   → 走査で見つけた世代を全部消せる。`cli:` に環境変数を渡す口が今は無いので設計が要る
3. 古い世代だけを別エントリ (`go-modcache-old`, `deleteVia: rm` + 強制) に分ける

いずれにしても**走査範囲と削除範囲を一致させる**のが不変条件。
`~/.claude/rules/verify-design-intent-before-refactor.md` に従い、着手前に方式の合意を取る。

## 変異検証の形

`~/go/A/pkg/mod` と `~/go/B/pkg/mod` を持つ偽 HOME で走査 → 削除 → 再走査し、
**候補に出た世代がすべて消えている**ことを assert する (fake の `go` は
`GOMODCACHE` に相当する 1 つだけを消す挙動を模す)。
変異 = 修正を戻す → 残存 1 件で red。

## 対応 (2026-09-04) — 案 3 の変形 (走査を落とさずに 2 エントリへ分ける)

**走査の範囲を削除の範囲へ合わせた。** 最小の修正方向 3 案のうち、1 (走査を絞る) は false green、
2 (世代ごとに `GOMODCACHE=<path> go clean`) は `Runner` が
`(ctx, name, args...)` で **env を渡す口が無く**、`cli:` は argv 直実行 (シェル無し) なので
`GOMODCACHE=… go clean` を argv として書けない — 型と全呼び出しに波及する設計変更が要る。
そこで **3 の変形**: 古い世代を rm するのではなく **`propose` (コマンドを出すだけ)** で見せる。

- `go-modcache` (現行): `Guard: GuardGoModcacheCurrent` で **`go env GOMODCACHE` の 1 世代だけ**を
  候補にする → `go clean -modcache` が全部消せるので **`OutcomeIncomplete` が出なくなる**
- `go-modcache-old` (使っていない世代): `Guard: GuardGoModcacheOld` + `DeleteVia: propose`。
  read-only で作られるので rm には強制が要る旨と、`chmod -R u+w` してから `rm -rf` する手順を
  `Detail` に書いた。**走査からは落とさない**ので false green にならない

guard の実装は `GuardVMRoot` の作法に揃えた (実効値と突き合わせ、**解決できないときは
候補 0 件ではなく「診断できず」へ倒す**)。`go env GOMODCACHE` の解決は `guards.do` で 1 回だけ。
`brewPrefix` と同じく、空 / 相対パス / `/` はエラーにする。

### 変異検証 (ケース名ごとの pass/fail。いずれも go build 成功を確認してから判定)

| 変異 | 世代分けのテスト | fail-closed のテスト |
|---|---|---|
| 条件を常に真にする (= 修正前の全世代を候補にする姿) | **FAIL** | PASS |
| 現行 / 古いの向きを反転 | **FAIL** | PASS |
| 解決できないとき候補 0 件で ok を返す (fail-closed を外す) | PASS | **FAIL** (4 ケース全部) |

⚠️ 最初の変異はビルドできなかった (`want` が未使用) ので当て直した。ビルド不能の緑を
「検知できず」と読まない (`mutation-verify-new-tests.md` の手順 1.5)。

### 副産物: 既存の安全機構が発火した

`TestOnlyReadOnlyCommands` (走査中に実行してよいコマンドの allowlist) が
`go env GOMODCACHE` で赤くなった。読み取り専用なので allowlist に足したが、
**新しい外部コマンドを走査に足すと機械が止める**ことが実地で確認できた。

### 満たせていない主張

- **案 2 (世代ごとの `go clean`) は未着手**。`Runner` に env を通す設計が要る。
  trigger: 破壊的操作の実行経路に env を渡す必要が他でも出たとき
- **実機での確認は未実施**。この開発機には 3 世代あるので `diskdoctor` を実行すれば
  分割が目で見えるが、走査に時間がかかるため人の確認に回す

