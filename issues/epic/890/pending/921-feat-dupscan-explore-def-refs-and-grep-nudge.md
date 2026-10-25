# 921 (feat): dupscan を探索に使わせる (定義を本体つきで返す・型の参照・grep からの案内)

> 保留 (2026-10-25): 実装・push は済み。残るのは受け入れ条件の最後の 1 つ (hook が効いたかの測り直し) だけ。**再開: 2026-11-25 ごろ**に下の「測り直しの手順」で測り、結果をここに書いて done へ移す

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

- [x] `dupscan def` が関数・初期化子・property と型を引き、型は宣言とメンバーの一覧、それ以外は本体を返す。`Type.member` とラベルで絞れる
- [x] `dupscan refs` が型・値としての参照を囲む単位つきで返し、コメント・文字列・宣言そのものを数えない
- [x] hook が `.dupscan.json` のある repo のシンボル探しの grep にだけ案内を出し、止めない。出しすぎない
- [x] 足したテストに変異を当てて赤を見る。`make -C src/dupscan lint test` と hook のテストが通る
- [x] 入口のドキュメントを直す
- [ ] 1 か月後 (2026-11-25 ごろ) に測り直し、ここに書く (hook が効いたかの判定)。全部の grep の回数ではなく、「識別子らしい grep」と「dupscan の def / refs / callers / index」の回数で比べる (反証レビューの指摘)

## 関連

- [899](899-research-dupscan-lsp-integration.md): LSP は使わない (ユーザー決定 2026-10-24)。本 issue は構文木の近似のまま引く手段を足す。
  同名の取り違えで判断を誤る実例が出たら 899 を再開する
- 2026-10-25 実装して push した (dotfiles `d8199d5b` まで def / refs、`afce63e5` まで hook)。~/dotfiles に取り込み、obaket で hook の案内と `dupscan def` が動くことを確かめた
  - commit: 「feat(dupscan): def (定義を本体つき・型はメンバーの一覧) と refs (識別子の参照) を足す」「fix(dupscan): def の型のメンバーに protocol の要件・格納 property・enum の case を含め、refs の宣言名の除外を広げる」
    「fix(dupscan): refs が戻り値の型・typealias の右辺・generic の制約の型を落としていたのを直す」「fix(dupscan): refs で enum の 2 つ目以降の case の名前を数えず、subscript の戻り値の型を数える」
    「feat(hooks): …grep したら dupscan を案内する」と hook の fix 3 本、「docs(skill/dupscan): 調べものの節に def と refs を足し、grep の前に使うと書く」
  - レビュー: issue の反証 (sonnet)・実装の敵対的レビュー 2 周 (codex の 5h 枠が超過していたので opus。ユーザー指示「codex ratelimit になったら opus で代わりにレビュー」)。
    採った主な指摘: refs が戻り値の型・typealias の右辺・generic の制約を落とす (この文法は name のフィールドをそれらにも付ける。木を出して確かめた。obaket の refs StorageLocation 2086 → 2230、grep 2243 行) /
    enum の case・protocol の要件がメンバーに出ない / hook が .swift の多い repo (obaket) で SIGPIPE + pipefail により一度も案内しない / 値を取るオプション・-i・Swift 以外の対象の偽陽性 / Grep ツールの経路の除外の漏れ。
    直さず README の限界に書いたもの: `def count()` が property・case にも当たる / `var x, y` の y / 複数行の case / `if let foo {` の省略形 / parameter pack の宣言
  - 変異 (想定のテストが赤): 宣言名の除外を外す・型名の正規化を外す・最初の name の子だけにする判定を全部の name に戻す・type_parameter を宣言の種類に戻す・extension の名前を宣言名に戻す・
    Outer.Inner の型の照合を外す (dupscan)。パイプの後ろの判定・Swift の repo の判定・出しすぎ防止・Type.member の大文字・SIGPIPE の判定・値をくっつけた -tswift (hook)。
    cwd でファイルかを確かめる判定を外す変異は、狙ったテスト (`..`) を含む多くのテストが赤になった (先に別のテストが落ちたので mutate-verify は rc=7)
  - 確認: `make -C src/dupscan lint test` rc=0・hook のテスト 43 passed・obaket の report の組は変更前の dupscan と同じ (238 組)
  - 測り直しの手順 (2026-11-25 ごろ): 直近 30 日の Swift の repo のセッションについて、Swift のパスを対象にした grep と dupscan の探索 (def / refs / callers / index。gate は数えない) を数える。
    2026-10-25 時点の比は grep 2,882 回 : dupscan 13 回 (数え方は「実測」節。母集合はほぼ obaket)。数えるスクリプト (2026-10-25 に使った形に、探索のサブコマンドだけを数える条件を足したもの):

    ```python
    import json, glob, os, re, time
    since = time.time() - 30 * 86400
    g = d = 0
    for f in glob.glob(os.path.expanduser('~/.claude/projects/*/*.jsonl')):
        if not re.search(r'obaket|DualNote|SnapTrim|ThumbnailThumb', f) or os.path.getmtime(f) < since:
            continue
        for line in open(f, errors='ignore'):
            if '"tool_use"' not in line:
                continue
            try:
                o = json.loads(line)
            except Exception:
                continue
            for c in (o.get('message') or {}).get('content') or []:
                if isinstance(c, dict) and c.get('type') == 'tool_use' and c.get('name') == 'Bash':
                    cmd = (c.get('input') or {}).get('command', '')
                    if re.search(r'\bdupscan\s+(def|refs|callers|index)\b', cmd):
                        d += 1
                    elif re.search(r'\b(grep|rg)\b', cmd) and re.search(r'\.swift|Sources|Tests|shared/|macOS/', cmd):
                        g += 1
    print('grep', g, 'dupscan', d)
    ```
