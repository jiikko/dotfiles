# 955 (research): dupscan を使って vlc-multi-video-player のテストの重複を解消したときの所見 (2026-10-28)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

起票日: 2026-10-28

## 概要

vlc-multi-video-player で「dupscan を使ってテストの重複を解消する」作業をしたときの、dupscan の所見の記録。
作業の結果は vlc commit `test: 保存が失敗する UserDefaults の test double を TestUserDefaultsStore に寄せる`。

- 測った環境: dupscan は dotfiles `e9a07e7f` (`src/dupscan` の最後の commit)、vlc の HEAD は `3112c09`
- **観察の記録で、仕様や設計の主張ではないので、反証レビューは通していない** (ユーザー指示 2026-10-28 の skill の規定)
- 既存の issue との重複照合は、954 を読んだだけ。F1 は **954 の P3 (読めない構文の原因の特定) の答えの 1 つ**
- 起票したセッションでは `src/dupscan` を直していない (別のセッション「Dupscan epic 890 continuation」が dupscan を触っているため)

## 集計

- dupscan を使った回数: 9 (report 3・sweep 1・def 2・callers 1・gate 1・`-v` の誤用 1)
- grep に戻した回数: 2。どちらも Swift のシンボル (conformer の一覧 `: UserDefaultsStoring`・key の定義 `selectedTab`) で、
  `dupscan refs` / `def` で引けた。理由は習慣 (hook が `dupscan def selectedTab` を案内したが、先に grep を打っていた)
- 役に立った場面: 2
  - `sweep` の節 2 (下限 12 節) が、既定の report で 0 組のテストから、`TestUserDefaultsStore` の写しの test double を見つけた。
    寄せた結果、写しの方が使った UserDefaults の suite を後片付けしていなかったことも直せた
  - `def VideoGridViewModelTestFactory.createForTest` で、テストの factory の違い (player が Stub か Mock か) を 1 回で読めた
- 所見: 4 件 (見逃し 1・雑音 1・使い方が分かりにくい 2)

## F1 (見逃し): 関数の中の `nonisolated(unsafe) let` が 1 行あると、ファイル全体が「読めない構文」になり比較から外れる

- **場面**: `dupscan -v .` (vlc の root)。「読めない構文があるファイル 10」と出た
- **原因の特定**: 10 ファイルのうち 9 ファイルは、関数の中で `nonisolated(unsafe) let` / `var` を宣言している
  (例: `nonisolated(unsafe) let callback = block`)。型の直下の `nonisolated(unsafe) private var` しか持たないファイル
  (`AppLogger.swift`・`ShortcutsSettingsViewModel.swift`) は読めている
- **最小の再現** (6 行。git の repo に入れて `dupscan -v .`):

  ```swift
  final class A {
      func run(_ block: @escaping () -> Void) {
          nonisolated(unsafe) let callback = block
          callback()
      }
  }
  ```

  → `読めない構文があるファイル: a/Local.swift`。同じ本体から `nonisolated(unsafe)` を外した関数と、型の直下の
  `nonisolated(unsafe) private var` を持つファイルは読める
- **残る 1 ファイル** (`VideoGridLoadingCoordinator.swift`) は `nonisolated(unsafe)` を含まない。原因は未調査
- **影響**: そのファイルの単位はすべて比較の外になる (1 行のせいで、そのファイルの他の関数も重複の判定から消える)。
  vlc では `MockVideoPlayer`・`MainActorTestCase`・`TestStorageFixture` など、テストの共通の部品の 3 ファイルが入っていた
- **出力**: `-v` は「読めない構文があるファイル: <path>」だけで、行も理由も出ない (1,352 B)。954 の P3 の obaket では「読めない:」の行が
  単位ごとに出ていたので、出方が違う (理由は未確認)
- **提案**: Swift の文法 (tree-sitter) が `nonisolated(unsafe)` を局所の宣言の修飾子として扱えるかを見る。
  直せないなら、ファイルの丸ごとではなく、読めない文を含む関数だけを外す。`-v` に行と構文の種類を出す

## F2 (雑音): `sweep` の節 2 (下限 12 節) の 20 組のうち、寄せる価値があったのは 1 か所 (3 組) だけ

- **場面**: `dupscan sweep VLCMultiVideoPlayerTests DomainTestSupport` (6,881 B)
- **実際**: 節 2 に 20 組。寄せたのは `TestUserDefaultsStore` の写しの `load` / `hasValue` / `remove` の 3 組 (同じ 1 つの型の写し)
- 残り 17 組の内訳 (どれも寄せない):
  - setUp の中の 1 行の closure (L1m)
  - builder の `with` が 8 個
  - 2 つの protocol の要件をそれぞれ VM へ転送する stub
  - 違う mock の `init` (値を受けて `super.init()` を呼ぶだけ)
  - 1 行の `makeGridViewModel`
  - 2 行の `tearDown`
- **提案**: 型の単位で「メンバーの多くが別の型の写し」と出せると、今回の 3 組は 1 つの候補 (「この test double は
  `TestUserDefaultsStore` の写し」) になり、上に並ぶ。今は 3 組が、1 行の closure の組と同じ並びに混ざる

## F3 (使い方が分かりにくい): `sweep` の節 3 の先頭が、2 文の setUp どうしの対 (`1.000 w=144`)

- **場面**: 同じ `sweep` の節 3 (部分木が似ている対) は 1 対だけで、`IndexMappingConsistencyTests.mainActorSetUp` ~
  `SwapSlotsSynchronousRegressionTests.mainActorSetUp` (`1.000 w=144`)
- **実際**: どちらも `viewModel = VideoGridViewModelTestFactory.createForTest()` と `viewModel.updateLayout(.twoByTwo)` の 2 文で、寄せる意味が無い
- `w=144` が何の重みかが出力にも `--help` にも見当たらず、2 文の本体で 144 になる理由が分からなかった (未確認: 部分木の節の数か)
- **提案**: `w` の意味を節の見出しに書く。同じ型の setUp の 2 文のような、文の数が少ない対は下げる
- 2026-10-28 追記: `w` は commit `feat(dupscan): sweep の節 3 を共有している重みの大きい順に並べる。946 に節 3 の上位を読んだ結果を記録する` (26ce1b2a) で入った
  「共有している重み」で、節 3 の並びの鍵だった。意味が出力と `--help` に出ないことは変わらない

## F4 (使い方が分かりにくい): `dupscan . -v` が「`-v` は git の repo の中ではない」と出て rc=2

- **場面**: 読めないファイルの一覧を見たくて `dupscan . -v` と打った
- **実際**: `dupscan: -v は git の repo の中ではない: git rev-parse --show-toplevel: exit status 128: fatal: cannot change to '-v'` (153 B、rc=2)
- skill dupscan には「flag は名前・パスの前に書く」とある。こちらの誤用だが、エラーは `-v` をパスとして読んだことしか言わず、flag の位置を直せばよいことが分からない
- **提案**: パスの位置に `-` で始まる語が来たら「flag はパスの前に書く (`dupscan -v .`)」と出す (926 で後置 flag を直した範囲に入っていない形かもしれない。未確認)

## 進捗

- [x] 起票 (2026-10-28)
- [ ] F1 の残り 1 ファイル (`VideoGridLoadingCoordinator.swift`) の原因
- [ ] 直す (F1 の文法・F4 のエラーの文言)。`src/dupscan` を触る他のセッションが居ないときに

- 2026-10-29: 954 を done にした。F1 の `nonisolated(unsafe) let` は 954 で回避済み (関数の中の出現を読み直す。読めないのはその関数だけで、ファイル全体が外れるわけではない)。同じ仕組みで空のタプルの引数 `f(())` も読めるようにした。
