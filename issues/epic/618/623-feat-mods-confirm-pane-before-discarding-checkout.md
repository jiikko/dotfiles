# 623 (feat): 未コミットの変更を捨てる checkout / restore の前に、mod で確認のペインを出す

> 🚨 **担当中: Claude code mods migration design (epic 618 を順に)**（2026-10-02〜）

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

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
- `claude -p` と bypass permissions のときに `ask` が何になるかは未確認。A を選ぶなら実測する

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

## 進捗

- 2026-10-02: 起票
