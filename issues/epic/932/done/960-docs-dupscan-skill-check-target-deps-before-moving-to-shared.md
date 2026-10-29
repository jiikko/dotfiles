# 960 (docs): skill dupscan の 4 節に「置き場所を compile する全 target が、移す型の依存を持っているか」を足す

起票日: 2026-10-29

959 の所見 5 の提案。obaket 1307 で、共通の test 置き場所 (`shared/Tests/SharedTestSupport/`) へ helper を移して **2 回ビルドが落ちた** (`CreditedTransferSleepGate` は `ObaketInfrastructure` の型、`InMemoryUploadBookmarkStore` は `ObaketPlatformContracts` の型に依存。その module を持たない test target も同じ置き場所を compile するため)。
原因は dupscan の外 (型を解決しないので検出できない。どの target が何を compile するかは `Package.swift` / `project.yml` の知識) なので、dupscan は直さず、寄せる人が読む skill dupscan の 4 節に確認項を足す。

ユーザーの承認: 2026-10-29 に dupscan-dogfood の報告で提案し、「issue にした上で対応して」と指示された。

## 変更

`_claude/skills/dupscan/SKILL.md` の 4 節に 1 項。移す前に、型が参照する module を、置き場所を compile する全 target が `Package.swift` (`project.yml`) で依存しているか確かめる。足りなければその module に依存する target の中に置く。
既存の「寄せ先は使う側のどれもが依存してよい層に」は層の話で、足したのは target の話、と書き分けた。

## 失うもの

無し (既存の手順に項を 1 つ足すだけ。4 節の他の項は変えていない)。

## 検証

結果は下の「結果」に書く。

## 結果

- 読み取りのみのサブエージェント 1 体に 3 観点 (skill 内 / 外 / 使う人) で反証させた。欠陥は無し、表現のずれ 3 点を直した:
  ①「全 target が compile する」は、実際は各 test target の `Helpers/SharedTestSupport` の symlink で決まる
  (実測 2026-10-29: ObaketCoreTests・ObaketInfrastructureTests・ObaketPresentationTests は symlink、ObaketPlatformTests・ObaketSFTPTests は `Asserts` と `Async` だけの実ディレクトリ)。
  項を「取り込む target を数える → その全部が依存しているか確かめる」の順に書き直した。
  ②「確かめる」が目視か build か曖昧だった。確かめる手段を `Package.swift` / `project.yml` の依存に絞り、「dupscan は落ちる前には見つけられない」と書き分けた。
  ③ Go の repo では `go.mod` と package の import で同じ確認をする、と 1 文足した。
- `bin/skill-eval dupscan`: PASS score=1 delta=1 cases=2/2 cost=$0.35。
- 未確認: 項が長めなことで読み流されるかどうかは測っていない (5 行 → 6 行になった)。
