# 1105 (docs): audit の polymorphism と leaky-abstraction が、抽象の層に散らばった網羅 switch を発見に数えるようにする

起票日: 2026-11-21

## 概要

audit skill (`_claude/skills/audit/SKILL.md`) の leaky-abstraction は、`default` の無い switch で case を並べるものを「偽陽性として報告しない」に入れている。
case を足せばコンパイルが書き漏れを止めるから、という理由。polymorphism には分類が無く、この除外とどう分けるかも書いていない。

このため、同じ判別子 (種別の enum) を抽象の層の何か所もで switch する形を、どの監査も数えない。case を 1 つ足すたびに N か所を書き換える形で、
書き漏れはコンパイルが止めても、書き換える場所の数は誰も止めない。trait を 1 つ足すたびに、同じ switch が 1 本ずつ増えていく。

polymorphism に発見の型を作り、この形を数えるようにする。leaky-abstraction の除外は「写像の 1 か所」に絞る。

## 報告 (原文・2026-11-21 ユーザー)

obaket で、抽象の層に具体クラスでの分岐が無いかを調べた (obaket epic 1569) 後の問い:

> 今調査依頼をしたような抽象化の崩れ始めみたいなコードスメルの観点は、dotfilesで管理しているauditにある観点に含まれている？

調べた結果を伝えた後の指示: 「dotfiles でissue化して直して」

調べた元の依頼 (obaket 1569 に原文):

> ポリモーフィズムみたいな感じで処理が分岐されるようにしたい. そういう具体クラスが書かれていると、そういうコードに足されていってしまうので、小さい時点から対処していきたい

## 今の形 (2026-11-21、`_claude/skills/audit/SKILL.md` version 1.6.0 を読んだ)

- **polymorphism** (forge のプロンプトと直接実行の指針): 「条件分岐でタイプや種別ごとに処理を分けている箇所を探し、ポリモーフィズムで置き換え可能な候補を特定する。
  特に、同じ条件が複数箇所に散在しているケースを優先する」。発見の型が無く、何を発見に数えないかも書いていない
- **leaky-abstraction** の「偽陽性として報告しない」: 「lint・型・exhaustive switch が既に止めている違反」と
  「具象 case ごとの表示名・フォームを `default` なし switch で列挙するもの (新 case で compile error になり追従が強制されるので**正当**)」。
  forge のプロンプトにも「lint・型・exhaustive switch が既に機械的に止めている違反は『発見』に数えない」がある
- 設計系をまとめて回すときは「1 つの発見は一番狭く当てはまる型 (L1〜L7 / E1〜E4 など) に 1 回だけ帰属させる」。polymorphism には型が無いので、
  種別の分岐は leaky-abstraction の L3 に帰属し、L3 の除外 (表示の出し分け・`default` なし switch) にそのまま落ちる

### 監査に数えられなかった実例 (obaket)

- obaket epic 1569 の棚卸し (2026-11-21): 抽象の層で provider の種別 (`ProviderConfig` / `StorageServiceKind` / `ProviderForm`) を switch する箇所が 53 か所。
  どれも `default` の無い switch で、今の除外に当たる。pCloud を足した commit は、無関係な sub-form 9 本にも case を書き足していた
- 同じ棚卸しで、どの型にも当たらない形が 2 つ見つかった
  - 自分以外の case を全部並べた arm (呼び出し側が型付きで渡せば届かない arm。9 本)
  - 種別ごとの値を Dictionary に並べた表 (`TransferSafetyPolicy.production`)。書き漏れた種別には表の外の共通の値 (`defaultMaxConcurrent` の 4) が何も言わずに使われ、コンパイルも止めない
- obaket の過去の監査 (`issues/audit-log` の 2026-08-26 と 2026-10-20) は polymorphism を 0 件と記録している。
  2026-08-26 の要約 (obaket 600) は、`default` の無い switch を「compile-time 防御として正当」として発見から外した

## 対応方針

- **polymorphism に発見の型を作る** (直接実行の指針。字下げした箇条で書く。`tests/claude/test_audit_skill_type_copies.sh` は行頭の `- **<id>**:` を ID として数える):
  - P1 散らばった網羅 switch: 同じ判別子を抽象の層の 2 か所以上で switch・`if case` している。`default` が無くコンパイルが書き漏れを止めても数える。
    測る量は、判別子ごとの分岐の数と「case を 1 つ足すと触るファイルの数」(直近に case を足した commit の変更ファイルを `git log` で数える。見積もりなら見積もりと書く)
  - P2 届かない arm: 呼び出し側がもう種別を絞っているのに、受け側がもう一度 switch し、自分以外の case を並べた arm を持つ (型付きで渡せば消える)
  - P3 種別の表の書き漏れ: Dictionary・配列で種別ごとの値を並べ、書き漏れた種別には表の外の共通の値か空が使われる。コンパイルが止めないので最優先
  - P4 1 か所の種別分岐: 上のどれでもないが、置き換えると分岐が消えるもの (今の説明の範囲)
- **正当として数えないもの** (polymorphism の偽陽性):
  - 判別子から型を引く写像 1 本 (種別ごとの型を返す switch。ポリモーフィズムの入口として必ず 1 か所残る)
  - 永続化の互換の境界 (保存された文字列から種別を決める decode)・外部入力の振り分け
  - 判別子の switch が抽象の層に 1 か所しか無いもの
- **修正の方向**: 種別ごとの型が protocol の要件として値を持ち、要件を満たす実装を protocol extension に置かない (書き漏れをコンパイルで止める性質を保つ)。
  辞書の登録 (registry) にしない (登録漏れが実行時まで見えなくなる)
- **寄せ先**: `kind == .x` / `is XxxImpl` / `as? XxxConcrete` で振る舞いを分ける形は今までどおり leaky-abstraction の L3。switch・`if case`・表の形は polymorphism の P1〜P4
- **leaky-abstraction の除外を絞る**: 「`default` なし switch で列挙するもの」は「写像の 1 か所」だけを正当とし、同じ判別子の switch が散らばっているものは polymorphism の P1 で数える、と書く。
  「exhaustive switch が既に止めている違反」も、止めているのは書き漏れだけで、散らばりは止めていないと書き分ける。forge のプロンプトも同じに直す

## 受け入れ条件

- [ ] polymorphism の forge のプロンプトと直接実行の指針に P1〜P4・偽陽性・寄せ先・各件に必須の項目がある
- [ ] leaky-abstraction の偽陽性の 2 項目と forge のプロンプトが、散らばった switch を polymorphism へ寄せる書き方になっている
- [ ] `tests/claude/test_audit_skill_type_copies.sh` が通る
- [ ] skill の改修なので、観点を変えた整合の点検を 3 回回す (`~/.claude/CLAUDE.md`「レビュー方針」) と、`bin/skill-eval audit` (`.claude/rules/skill-eval-after-edit.md`)

## 関連

- obaket epic 1569 (`apps/obaket/issues/epic/provider-dispatch/`) — この改修のきっかけになった棚卸し
- `_claude/rules/comment-no-restate-enforced.md` — 「enum を registry / plugin に作り替えるな」をコメントに残す例。この issue の修正の方向 (registry にしない) と食い違わない
