# 623 (feat): 未コミットの変更を捨てる checkout / restore の前に、mod で確認のペインを出す

起票日: 2026-10-02

epic [618](../618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

`_claude/hooks/warn-discarding-checkout.sh` (issue 297) は、未コミットの変更を捨てる形の `git checkout` / `git restore` の直前に、
何が消えるかを**モデルへの注意として注入する**。deny にしないのは、変異検証の復元が正当な用途だから (script 冒頭)。
この形では、消えるかどうかの判断はモデルに任されたままで、人には何も見えない。

人に選ばせる形は 2 通りある:

- **A. settings の hook のまま `ask` を返す**: PreToolUse で `permissionDecision: ask` を返し、理由に消える差分 (`git diff --stat -- <path>`) を載せる。
  標準の確認ダイアログで人が選ぶ。mod の失敗モードが増えない
- **B. mod**: 記事の Blast Radius の形。`tool.call` で止め、差分をペインに出し、進める / 止めるのボタンで選ぶ。A より多くを見せられる

**A で足りるなら A にする** (mod にする理由は「A では見せきれないものがある」と言えるときだけ)。

## 先に決めること: 人が応答できないセッションをどう見分けるか

`warn-discarding-checkout.sh` は、意図して粗く判定している (冒頭。ブランチの切り替えも「出す側へ倒す」)。
deny にしないのは、変異検証の復元が正当な用途だから。A でも B でも、人を待たせる形にすると、
偽陽性と正当な復元のたびに止まる。

- **A (settings の hook) は PG に届かない**。PG は `--setting-sources project,local` でユーザーの settings の hook を外して起動する (618 の前提)
- **B (mod) は、PG に載れば永久に待つ**。PG は対話モードで動くので、対話かどうかでは見分けられない。619 で PG に mod を載せないと決めればこの心配は消える
- `claude -p` と bypass permissions のときに `ask` が何になるかは未確認。A を選ぶなら実測する → 実測した (下の「実測」。`-p` では測った 4 モードとも拒否)

- PG などを見分ける手段 (環境変数・`--settings` で渡す印 等) を先に決める。決められなければ、人を待たせる形にはせず今の注入のままにして閉じる
- 偽陽性を減らす (ブランチの切り替えを判定から外す) かどうかも合わせて決める

## 対応方針

以下は B を選んだ場合:

- `_claude/mods/discard-guard/`: `tool.call` (`{ tool: 'Bash' }`) で、捨てる形の checkout / restore を検出する
- **検出の正本は既存の script に残す**。`warn-discarding-checkout.sh` の判定 (script 全体で 190 行) を TS へ写すと 2 実装になる。
  `$.process.run` で判定だけを呼ぶ形にできるか、まず確かめる (script に判定だけを返す入口が要るかもしれない)
- 消える変更が 0 件なら何も出さずに通す
- ペインが置けない (端末が狭い) ときの扱い: 記事は帯に降格する例を出している。降格するか、今の注入に戻すかを決める
- ペインが開ける条件 (端末の幅・全画面か) を reference.md と型で確かめてから、置けないときの扱いを決める

## 決めること

- [ ] settings の `warn-discarding-checkout.sh` を外すか、残して二重にするか。注入はモデルへの説明として残す価値がある。
      二重にするなら、両方が同じ呼び出しで出たときの見え方を確かめる
- [ ] ペインの見た目。決める前に見本を出す (`decide-layout-in-sample-renderer-first.md`)

## 実測 (2026-10-02 / claude 2.1.287)

- **A (settings の hook の `ask`) を `claude -p` で返すと、モードによらず拒否になる**。PreToolUse (matcher Bash) で
  `permissionDecision: "ask"` + 理由を返す hook を `--settings` で渡し (`--setting-sources project,local`、haiku、各 1 回)、`touch <印>` を頼んだ。`--permission-mode` が
  default / bypassPermissions / acceptEdits / auto の 4 通りとも (plan / dontAsk は未測定)、印は作られず (コマンドは走らない)、`permission_denials` に載り、モデルは理由文を受け取った。
  待ち続けることはない。→ headless の経路では「人を待たせる」にならず、今の注入 (走らせたうえで注意を渡す) より強い「止める」になる
- **B (mod の `tool.call`) はこのマシンで届く** (619 の probe。`tool.call` は bypass されない)。`classic.PreToolUse` は bypass されるので、mod で止めるなら `tool.call` で `{ deny }` を返す形になる
- PG は今もユーザーの settings の hook が走らない (431) ので、A は PG に効かない (今の注入も効いていない = 退行ではない)。B は 619 の決定で PG に mod を載せないので、これも PG に効かない
- 対話のセッションで `ask` が標準の確認ダイアログになるか (auto mode でも出るか) は未実測

- **A の対話での見え方 (実測)**: 隔離した tmux で、Bash を `--allowedTools` で許可した対話のセッションに、PreToolUse で `ask` + 理由を返す hook を `--settings` で渡し、
  `echo` を頼んだ。許可済みのツールでも標準の確認 (`Hook PreToolUse:Bash requires confirmation for this command:` + 理由文 + `Do you want to proceed? 1. Yes / 2. No`) が出た。
  理由文には何が消えるか (`git diff --stat` 等) を載せられるので、「A では見せきれないもの」は今のところ見当たらない → mod (B) にする理由は無い
- **残る判断 (ユーザーに聞く)**: `warn-discarding-checkout.sh` は issue 297 で「変異の復元は正当な用途なので deny にしない。permissionDecision を返さず、許可の判断を変えない」と意図して決めた。
  `ask` にすると、対話では捨てる形の checkout のたびに人の確認が要り (auto mode でも出る)、`claude -p` では拒否になる。`git checkout foo` (ファイルを捨てるかブランチの切り替えか、静的に区別できない) も出す側に倒しているので、偽陽性のたびに止まる
  - 案 1: 確実に捨てる形 (`git checkout -- <path>` / `git restore <path>`。事故 3 件はすべて `checkout --`) だけ `ask`、曖昧な `git checkout <x>` は今の注入のまま
  - 案 2: 今の注入のまま (623 を「移さない」で閉じる)
  - 案 3: 判定に当たるもの全部を `ask` にする

## 決着 (2026-10-02): mod は使わず、settings の hook のまま「確実に捨てる単純なコマンドだけ ask」にした

ユーザーの判断 (2026-10-02): 確実に捨てる形だけ ask、曖昧な `git checkout <x>` は今の注意のまま。A (settings の `ask`) で理由に消える一覧を見せられるので、mod (B) にはしない。

- **ask の範囲 (最終)**: コマンド全体が 1 つの単純な git コマンド (`;` `&` `|` 改行・引用符・heredoc・コメント・`$(…)`・`\`・`>(` `<(` が無い、git が先頭の語)、
  cd / pushd / popd / `GIT_DIR=` / `GIT_WORK_TREE=` / `--git-dir` / `--work-tree` が無い、`-C` が 1 つ以下でその dir が在り、その repo の作業ツリー側に追跡中の変更があるもの。
  形は `checkout -- x` / `.` / `-f` / `--force` / `-p` / `--patch` / 束ね (`-fq` 等。`b` / `B` より前の文字だけを見る)、`restore` の作業ツリー。
  それ以外は今までどおり注意 (additionalContext) だけ。分類の表と 37 形の実測は hook のヘッダ
- 注意は、捨てる segment ごとにその `-C` か cwd を見て、変更のある repo の一覧をすべて出す (旧は最初に現れた `-C` の 1 つだけを見ていた)
- ask のときもモデルに同じ一覧を `additionalContext` で渡し、理由の末尾に「拒否されたら別のコマンド (reset --hard / stash / show で上書き) で同じことをしない」を添える
- テスト 94 件 (`tests/claude/test_warn_discarding_checkout.sh`)。変異は延べ 26 本で red を確認

**敵対的レビュー (opus、5 周)**:
- 1 周目: 引用符・heredoc の中の文字列 (commit message 等) で ask になり `claude -p` では commit ごと拒否される (P2) ほか → 引用符と heredoc を字句で取り除いて判定する近似を入れた
- 2 周目: 近似が別の入力で破られた (コメントの中で ask・引用符の中の `; cd` で対象を奪われて黙る・`<<'MSG-EOF'`・perl の 2 乗の遅さ)
- 3 周目: 近似をやめて「単純なコマンドだけ ask」に軸を移した後、cd の追跡が退行を作った (後ろの cd に最初の破棄の対象を奪われて黙る / 解決できない対象のまま cwd の一覧で ask)
  → cd の追跡をやめ、対象に自信が無い形は ask にしない
- 4 周目: 対象の repo をコマンドに 1 つしか持たないための退行 (後ろの `-C <dirty>` を見落として黙る / clean な別 repo への破棄で cwd の一覧で ask) → segment ごとに dir を持ち、
  さらに ask をコマンド全体が 1 つの git コマンドのときだけに絞った
- 5 周目: 「旧で注意が出ていた形が黙る」は作れなかった。無害な形で ask になる P3 が 4 件 (`-bfix` の名前の f・index にだけ変更・`-f -h`・`-sWIP`) → 字句の読み方を直した。
  判定のロジックは新設せず、各直しをテストと手での実測で確かめたので、6 周目は回さずに閉じた (規約の打ち切りの例外)
- 学び: 字句の gate の迂回を 1 つずつ塞ぐと終わらなかった。「判定できる形だけ止め、それ以外は今までどおり」に軸を移してから収束した

**未確認 (再開の trigger)**:
- 対話の auto mode と、サブエージェントの中で ask がどう見えるか (対話の default mode と `claude -p` の 4 モードは実測)。サブエージェントで拒否になるなら、正当な復元が止まる → 起きたらこの issue に書く
- 大きな repo や多数の `-C` を繋いだコマンドで、dir ごとの `git status` が hook の上限 (10 秒) を超えるか (小さな repo では旧 0.07 秒 / 新 0.08 秒)
- ask は repo 単位で判定する (`git checkout -- x` で x に変更が無く、他のファイルに変更があるときも ask)。害は確認 1 回


## 進捗

- 2026-10-02: 起票
- 2026-10-02: 起票と同じ日に、反証レビュー (sonnet 2 本) と敵対的レビュー (opus 2 本) の指摘で方針を改訂した。623 を、settings の `ask` (A) で足りるかを先に決める形に直し、PG は今も user の hook が走らないこと・B が PG に載れば永久に待つこと・偽陽性を足した。採否と理由の一覧は親 618 の進捗
- 2026-10-02: A (settings の `ask`) を `claude -p` の 4 モードで実測 (どれも拒否になり、待たない)。B に要る `tool.call` はこのマシンでも mod に届く。対話での `ask` の見え方は未実測
- 2026-10-02: 決着 — 確実に捨てる単純なコマンドだけ settings の hook の ask にした (ユーザーの判断)。敵対的レビュー 5 周、テスト 94 件。mod は使わない
