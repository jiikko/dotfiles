# 623 (feat): 未コミットの変更を捨てる checkout / restore の前に、mod で確認のペインを出す

起票日: 2026-10-02

epic [618](618-design-claude-code-mods-migration.md) の子。619 の後。

## 概要

`_claude/hooks/warn-discarding-checkout.sh` (issue 297) は、未コミットの変更を捨てる形の `git checkout` / `git restore` の直前に、
何が消えるかを**モデルへの注意として注入する**。deny にしないのは、変異検証の復元が正当な用途だから (script 冒頭)。
この形では、消えるかどうかの判断はモデルに任されたままで、人には何も見えない。

mod なら、記事の Blast Radius の形にできる: `tool.call` で止め、消える差分 (`git diff --stat -- <path>`) をペインに出し、
進める / 止めるのボタンで人が選ぶ。

## 対応方針

- `_claude/mods/discard-guard/`: `tool.call` (`{ tool: 'Bash' }`) で、捨てる形の checkout / restore を検出する
- **検出の正本は既存の script に残す**。`warn-discarding-checkout.sh` の判定 (173 行) を TS へ写すと 2 実装になる。
  `$.process.run` で判定だけを呼ぶ形にできるか、まず確かめる (script に判定だけを返す入口が要るかもしれない)
- 消える変更が 0 件なら何も出さずに通す
- ペインが置けない (端末が狭い) ときの扱い: 記事は帯に降格する例を出している。降格するか、今の注入に戻すかを決める
- 人が応答できないセッション (`claude -p`、pro-con の PG) では、ペインで止めず今の注入の挙動にする (止めると永久に待つ)

## 決めること

- [ ] settings の `warn-discarding-checkout.sh` を外すか、残して二重にするか。注入はモデルへの説明として残す価値がある。
      二重にするなら、両方が同じ呼び出しで出たときの見え方を確かめる
- [ ] ペインの見た目。決める前に見本を出す (`decide-layout-in-sample-renderer-first.md`)

## 進捗

- 2026-10-02: 起票
