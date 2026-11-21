# 1105 (docs): audit の polymorphism と leaky-abstraction が、抽象の層に散らばった網羅 switch を発見に数えるようにする

起票日: 2026-11-21

## 概要

audit skill (`_claude/skills/audit/SKILL.md`) の polymorphism は「同じ条件が複数箇所に散在しているケースを優先する」と書くが、発見の型も「何を数えないか」も持っていない。
一方 leaky-abstraction は、「exhaustive switch が既に止めている違反」と「`default` なし switch で表示名・フォームを列挙するもの (正当)」を偽陽性に入れている。

このため、同じ判別子 (種別の enum) を抽象の層の何か所もで switch する形は、「`default` が無いので正当」と読まれて落ちやすい。
コンパイルが止めるのは case の書き漏れで、種別を 1 つ足すたびに書き換える場所の数ではない。trait を 1 つ足すたびに、同じ switch が 1 本ずつ増えていく。
どの観点も「種別を 1 つ足すと何ファイル触るか」を測らない。

polymorphism に発見の型 (D1〜D4) と数えないものの基準を作り、leaky-abstraction の除外は「写像の 1 か所」に絞る。

## 報告 (原文・2026-11-21 ユーザー)

obaket で、抽象の層に具体クラスでの分岐が無いかを調べた (obaket epic 1569) 後の問い:

> 今調査依頼をしたような抽象化の崩れ始めみたいなコードスメルの観点は、dotfilesで管理しているauditにある観点に含まれている？

調べた結果を伝えた後の指示: 「dotfiles でissue化して直して」

調べた元の依頼 (obaket 1569 に原文):

> ポリモーフィズムみたいな感じで処理が分岐されるようにしたい. そういう具体クラスが書かれていると、そういうコードに足されていってしまうので、小さい時点から対処していきたい

## 今の形 (2026-11-21、`_claude/skills/audit/SKILL.md` version 1.6.0 を読んだ)

- **polymorphism** (forge のプロンプトと直接実行の指針): 「条件分岐でタイプや種別ごとに処理を分けている箇所を探し、ポリモーフィズムで置き換え可能な候補を特定する。
  特に、同じ条件が複数箇所に散在しているケースを優先する」。発見の型と「数えないもの」が無い
- **leaky-abstraction** の「偽陽性として報告しない」: 「lint・型・exhaustive switch が既に止めている違反」と
  「具象 case ごとの表示名・フォームを `default` なし switch で列挙するもの (新 case で compile error になり追従が強制されるので**正当**。違反は `==` で振る舞いを分ける方だけ)」。
  forge のプロンプトにも「lint・型・exhaustive switch が既に機械的に止めている違反は『発見』に数えない」がある
- 設計系をまとめて回すときは「1 つの発見は一番狭く当てはまる型 (L1〜L7 / E1〜E4 など) に 1 回だけ帰属させる」。種別の分岐は、型を持つ leaky-abstraction の L3 に帰属しやすく、
  そこで L3 の除外に当たる

### obaket の実例

- obaket epic 1569 の棚卸し (2026-11-21): 抽象の層で provider の種別 (`ProviderConfig` / `StorageServiceKind` / `ProviderForm`) を switch する箇所が 53 か所。
  `default` のあるものは 3 か所だけで、50 か所は `default` の無い switch。pCloud を足した commit は、無関係な sub-form 9 本にも case を書き足していた
- 同じ棚卸しで見つけた、散らばり以外の形:
  - 自分以外の case を全部並べた arm (呼び出し側が型付きで渡せば届かない arm。9 本)
  - 種別ごとの値を Dictionary に並べた表 (`TransferSafetyPolicy.production`)。書き漏れはコンパイルが止めず、表の外の共通の値 (4) が使われる。
    ただし、全 kind を回して欠落を `Issue.record` するテスト (`TransferBandwidthSettingsStoreTests`) がある
- obaket の過去の監査の扱いは割れている
  - 2026-05-20 の監査 (obaket 110) は、`StorageNamespace` の switch が 4 か所に散らばっているのを polymorphism の発見として報告した
  - 2026-08-26 の要約 (obaket 600) は、provider の主要な enum の `default` の無い switch を「compile-time 防御として正当」として発見から外した
  - 2026-10-20 の監査 (obaket 1216) は「provider の `==` 分岐は lint の例外だけ」として、polymorphism を新規 0 件とした (skill の本体が無い環境で、過去の記録から手順を組み直した回)
  - どれも今の 1.6.0 より前のバージョンで回した。読み手が leaky-abstraction の除外を polymorphism にも当てうる、という形の実例として挙げる (1.6.0 の文言がそう強制しているわけではない)

## 対応方針

- **polymorphism に発見の型を作る** (直接実行の指針。字下げした箇条で書く。`tests/claude/test_audit_skill_type_copies.sh` は行頭の `- **<id>**:` を ID として数える)。
  記号は D にする (P1〜P3 はレビュー・監査の重要度と同じ字面。1088 で entry-parity の型を P から N に改めたのと同じ理由):
  - D1 散らばった種別の分岐: 同じ判別子の分岐が 2 か所以上にあり、各分岐が**種別そのものの性質** (どの呼び出し側から見ても同じ答え) を決めている。構文は問わない。
    同じ判別子の分岐の集まりを 1 件にまとめ、その中の届かない arm (D2) もこの件に書く
  - D2 届かない arm: 呼び出し側が種別を絞って渡しているのに、受け側がもう一度分岐し、自分以外の種別を並べた arm を持つ。単独の件にするのは、判別子の分岐がその 1 組だけのとき
  - D3 種別の表の書き漏れ: Dictionary・名指しの配列に種別ごとの値を持ち、書き漏れた種別には表の外の値か空が使われる。全種別を回して欠落を落とすテストがあるかを確かめて書く
  - D4 その他の種別分岐: D1〜D3 に当たらないが、置き換えると分岐が消えるもの。1 か所で抽象の具象を名指しして振る舞いを分ける形は leaky-abstraction の L3 が先に取る
- **数えないもの** (偽陽性を量産しないために):
  - 種別の集合が閉じていて増えない判別子 (構文木の node・プロトコルの状態・固定の状態機械)。増えるかは、種別を足した commit と issue で確かめる
  - 各分岐が呼び出し側ごとに違うことを決めているもの (dotfiles の dupscan の `calleeIdent` と `funcName` は同じ `ast.Expr` を型 switch するが、求めるものが違う)
  - 写像 1 本・永続化の互換の decode・外部入力の振り分け・表示の分岐が 1 か所だけのもの・種別ごとのファイルの中で自分の種別だけを読む分岐・テストの入力を作る分岐
  - 理由コメントや過去の監査の判断が今も成立しているもの (dotfiles 886 の I5: `ticketState` の 3 か所の分岐を「薄い置き換えは複雑性を移すだけ」とし、4 つ目の state を足すときを trigger にした)
- **各件に必須**: 判別子と、その集合が増えるかの根拠 / 分岐の数と母集合 (case 名で拾った候補は 1 つずつ読んで型を確かめる) / 種別を 1 つ足すと書き換える**既存の**ファイルの数
  (新しく作ったファイルを除く。pCloud の commit は 76 ファイルを変えたが、新しい client・adapter・テストを含む) / silent に壊れるか compile error・テストで止まるか / 寄せ先 /
  置き換えで良くなること (`verify-design-intent-before-refactor.md` の問い。どれも良くならない候補は報告しない)
- **修正の方向は言語ごとに書き分ける**: 書き漏れを型かテストで止める形を、その言語で選ぶ (Swift の protocol の要件・TypeScript の `satisfies Record<Kind, …>`・型で止められない言語は全種別を回すテスト)。
  辞書の登録 (registry) にするなら、全種別を回すテストを同時に置く (dotfiles 006 は zsh の denoise の検証と dispatch を連想配列 1 つに寄せる案を採っている)
- **寄せ先**: 1 か所の具象の名指しの振る舞いの分岐は leaky-abstraction の L3。同じ判別子の分岐が 2 か所以上なら、構文によらず D1 にまとめる
- **leaky-abstraction の除外を絞る**: 「`default` なし switch で列挙するもの」は「写像の 1 か所」だけを正当とし、散らばりは polymorphism の D1 で数える、と書く。
  「exhaustive switch が既に止めている違反」も、止めているのは書き漏れだけで、散らばりは止めていないと書き分ける。forge のプロンプトも同じに直す
- **forge と直接実行の食い違いを作らない**: forge の polymorphism のプロンプトは型の名前だけを並べ、型の定義・数えないもの・必須の項目・寄せ先は直接実行の指針を正本として参照させる

## 受け入れ条件

- [x] polymorphism の forge のプロンプトと直接実行の指針に D1〜D4・数えないもの・寄せ先・各件に必須の項目がある (forge は直接実行の指針を参照)
- [x] leaky-abstraction の偽陽性の 2 項目と forge のプロンプトが、散らばった分岐を polymorphism へ寄せる書き方になっている
- [x] `tests/claude/test_audit_skill_type_copies.sh` が通る
- [ ] skill の改修なので、観点を変えた整合の点検を 3 回回す (`~/.claude/CLAUDE.md`「レビュー方針」) と、`bin/skill-eval audit` (`.claude/rules/skill-eval-after-edit.md`)

## 反証レビュー (codex gpt-6.2-sol high、2026-11-21。対象は issue の最初の書き方と SKILL.md の最初の改修)

P1 2 件・P2 5 件・P3 2 件。引用されたコードと記録を読んで裏を取り、全部採った (却下 0)。

| 指摘 | 重さ | どう直したか |
|---|---|---|
| 分類を必須にしたのに、P1 と P2 が同じ判定を二重に取り、内部の enum の `if/else` が 2 関数に散る形はどこにも入らない | P1 | 同じ判別子の分岐の集まりを D1 の 1 件にまとめ、D2 はその中に書く。D4 を「その他」にして取りこぼしを無くした |
| P1 が、同じ値を使う別の処理 (Go の `calleeIdent` / `funcName`・zsh の `__AV1IFY_LOCK_MODE`・エラーを再試行と表示で別に扱う形) を誤検出する。886 の I5 の判断とも食い違う | P1 | D1 の条件に「種別の集合が増える」「各分岐が種別そのものの性質を決める」を足し、閉じた集合・呼び出し側ごとの処理・過去の判断を「数えない」に入れた |
| 「1.6.0 ではどの監査も数えない」は誤り (polymorphism は散在を優先と書き、obaket 110 は散らばった switch を報告した) | P2 | 概要と「obaket の実例」を、除外が読み手に当てられうる形の実例として書き直した |
| 「53 か所はすべて `default` なし」は誤り (3 か所にある)。過去の監査は 1.6.0 より前 | P2 | 50 / 3 に直し、バージョンの前後を書いた |
| P4・表示の除外・L3・N2 の寄せ先が揃わない。`if x is Foo` と `case is Foo` で寄せ先が変わる | P2 | 寄せ先を「1 か所は L3、2 か所以上は構文によらず D1」に統一し、N2 への寄せを消した |
| P3 の「最優先」の根拠にした `TransferSafetyPolicy` には、欠落を落とすテストがある | P2 | D3 を「テストの有無を確かめて書く」に変え、最優先の語を外した |
| 修正の方向を Swift の protocol に決め打ちすると、Go・shell・TypeScript や dotfiles 006 の採った設計に合わない | P2 | 言語ごとに書き分け、registry にするなら全種別を回すテストを置く形にした。`verify-design-intent-before-refactor.md` の問いを必須の項目に入れた |
| forge のプロンプトが表を探さないのに P3 を求め、除外・寄せ先も省いている | P2 | forge は型の名前と表を探すことだけを書き、定義・除外・必須・寄せ先は直接実行の指針に従わせた。leaky-abstraction の forge のプロンプトに写像の 1 か所の除外を足した |
| 計測の手順で分岐の数が合わない (同じ関数の複数の switch が 1 件になる・別の enum が混ざる)。commit の全変更ファイル数は過大 | P3 | 候補を読んで型を確かめ、同じ関数の分岐も別に数える。既存のファイルだけを数える |
| P1〜P4 は重要度と同じ字面 (1088 で N に改めた理由) | P3 | D1〜D4 に改めた |

反証できなかったもの: 1.6.0 の文言の引用・obaket の 53 か所と 9 本・`ProviderForm.apply` の絞り込み・Dictionary の欠落が 4 に落ちること・
comment-no-restate-enforced の registry の例との整合・912 (保留) との衝突が無いこと。

## 関連

- obaket epic 1569 (`apps/obaket/issues/epic/provider-dispatch/`): この改修のきっかけになった棚卸し
- 1088 (entry-parity を足した回。型の記号を P から N に改めた理由)
- 886 の I5 (散らばった状態の分岐を、置き換えずに trigger を書いた判断)
