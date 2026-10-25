# 921 (feat): dupscan を探索に使わせる (定義を本体つきで返す・型の参照・grep からの案内)

> 🚨 **担当中: M4Pro-MacBook-Pro (glogx)**（2026-10-25〜）

起票日: 2026-10-25

## 概要

Swift の repo で定義・呼び出し元を探すとき、agent は dupscan ではなく grep を使っている。grep はコメント・文字列・呼び出し側の行が混ざり、
返る量が多い。dupscan を探索の道具として使わせるために、①足りない機能 (定義を本体つきで返す・型を含む参照の検索) を足し、
②Swift の repo でシンボル名を grep したときに dupscan を案内する hook を足す。ユーザー指示 (2026-10-25):「lsp みたいに定義元にジャンプしたり、
呼び出し一覧みたいに探索として使うユースケースのために拡張するのはあり？llm のログを見ていると grep しているので dupscan の方が効率よくねえか」
「dupscan を使われるための施策、dupscan への足りない機能の実装を進めて」。

## 実測 (2026-10-25)

`~/.claude/projects/` の Swift の 4 repo (obaket・DualNoteApp・SnapTrim・ThumbnailThumb) の直近 7 日のセッションのうち、dupscan を作っていたセッションを除く 27 本
(反証レビューの指摘: 直近 7 日にセッションがあったのは実質 obaket 系のディレクトリだけで、4 repo の内訳ではない。パスの条件は `macOS/issues`・docs・`.md` も拾い、15% 前後が混ざる):

| 数えたもの | 回数 |
|---|---|
| Swift のパス (`.swift`・`Sources`・`Tests`・`shared/`・`macOS/`) を対象にした grep / rg の Bash | 2,882 |
| dupscan の Bash | 13 (7 セッション) |

grep のパターンには `func executeMutation`・`struct HTTPRequestHead`・`retrying(`・`StorageOperationLockRegistry` のような、定義や呼び出し元を探すものが多い
(識別子らしいパターンは全体で 1,174。数え方は正規表現の近似で、Swift 以外の識別子 (`run_cli` 等) も混ざる)。
skill dupscan の 2 節に「grep の前に」と書いてあっても使われていないので、機能を足すだけでは効かない (②が要る)。

## 対応方針

### 1. `dupscan def <名前>` — 定義を本体つきで返す (LSP の「定義へ移動」の代わり)

- 名前は完全一致 (大文字小文字を区別する)。`Type.member` で囲む型を絞る (新しい書き方。callers は `Type.member` を受け付けない)。`make(x:_:)` で引数ラベルを絞る (こちらは callers と同じ書き方)
- 関数・初期化子・property・subscript は `file:line-endLine 型.署名` の見出しに続けて本体の行を出す (長い本体は `-max-lines` で切り、切ったことを出す)
- **型 (struct / class / enum / protocol / actor / extension) も引けるようにする**。いまの単位は型の宣言を持たないので、adapter が型の宣言 (名前・種類・行の範囲) も集める。
  型は全文を出さず、宣言の行と、中のメンバーの一覧 (index -file と同じ 1 行の形) を出す (数千行の型を丸ごと出さない)
- 同名が複数あれば全部出す (型を解決しないので選ばない)。`-format json` も出す

### 2. `dupscan refs <名前>` — 参照の一覧 (LSP の「参照を検索」の代わり)

- 識別子としての出現 (型としての出現 = `type_identifier`、値としての出現 = `simple_identifier`) を、それを囲む単位つきで返す。コメントと文字列は構文木に出ないので混ざらない
- callers は呼び出しだけ。refs は型の注釈・継承・`Type.static` の参照・引数として渡す関数名も拾う。宣言そのもの (`struct Foo` の Foo) は除く
- 出力は callers と同じ形 (`file:line 囲む単位 [test] | その行`) に、構文上の位置 (`[型の位置]` / `[式の位置]`) を足す。型名も式の中 (`Foo(…)`・`Foo.shared`) では式の位置に出る (反証レビューの指摘で「型 / 値」から改めた)

### 3. hook: Swift の repo でシンボル名を grep したら dupscan を案内する

- PreToolUse (Bash と Grep)。git が .swift を追跡している repo (反証レビューの指摘: `.dupscan.json` があるのは obaket だけで、dupscan は設定が無くても動く) で、grep / rg / git grep のパターンが識別子らしい
  (`func X`・`struct X`・`X(`・単語 1 つの識別子) ときに、additionalContext で `def` / `refs` / `callers` を 1 行案内する。**止めない** (grep は文字列・コメント・設定ファイルを探すのに要る)
- 同じセッションで出しすぎない (間隔を空ける)。検出しない形と偽陽性はスクリプト冒頭に書く

### 4. 入口のドキュメント

skill dupscan の 2 節・README・obaket の AGENTS.md / CLAUDE.md の dupscan の節に `def` / `refs` を書く。

## 受け入れ条件

- [ ] `dupscan def` が関数・初期化子・property と型を引き、型は宣言とメンバーの一覧、それ以外は本体を返す。`Type.member` とラベルで絞れる
- [ ] `dupscan refs` が型・値としての参照を囲む単位つきで返し、コメント・文字列・宣言そのものを数えない
- [ ] hook が `.dupscan.json` のある repo のシンボル探しの grep にだけ案内を出し、止めない。出しすぎない
- [ ] 足したテストに変異を当てて赤を見る。`make -C src/dupscan lint test` と hook のテストが通る
- [ ] 入口のドキュメントを直す
- [ ] 1 か月後 (2026-11-25 ごろ) に測り直し、ここに書く (hook が効いたかの判定)。全部の grep の回数ではなく、「識別子らしい grep」と「dupscan の def / refs / callers / index」の回数で比べる (反証レビューの指摘)

## 関連

- [899](pending/899-research-dupscan-lsp-integration.md): LSP は使わない (ユーザー決定 2026-10-24)。本 issue は構文木の近似のまま引く手段を足す。
  同名の取り違えで判断を誤る実例が出たら 899 を再開する
