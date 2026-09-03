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
