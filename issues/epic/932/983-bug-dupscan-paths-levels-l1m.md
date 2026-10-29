# 983 (bug): paths の除外の levels が L1m を粗い側として扱わない

起票日: 2026-10-30
優先度: 低 (L1m は opt-in の段。既定の report / gate の rc には効かない)

## 概要

`.dupscan.json` の paths の除外に `levels` を書くと、`internal/exclude/exclude.go` の `Rule.check` は「細かい段だけを外しても粗い段の組として出るので効かない」として、
粗い側の段を含めることを求める。ところが粗い側の判定は `norm.Levels` (L1・L2・L3) だけを見るので、L1 より粗い L1m (L1 で一致すれば L1m でも一致する) を求めない。

- `levels: ["L1", "L2", "L3"]` は通り、その glob の下の写しが `-levels L1,L2,L1m` と sweep の節 2 に出続ける (levels を省いた既定は L1m を含めている。`check` のコメントがこの理由を書いている)
- `levels: ["L1m"]` だけの除外も通る (既定の段には何も効かない除外。単位に当たれば `used` になるので「効いていない」にも出ない)
- 新しく足された除外の要素 (`Rule.elements`) も段の並びを `norm.Levels` だけで書くので、L1m を足した・外した変更が要素の違いに出ない

## 再現 (2026-10-30、使い捨て repo)

`g/A.go` と `g/B.go` に `os.Stdout` と `os.Stderr` だけが違う同じ本体の関数を置き、`.dupscan.json` に
`{"paths":["g/**"],"levels":["L1","L2","L3"],"kind":"intentional","reason":"t"}` を書く。

- `dupscan report -levels L1,L2,L1m .` → `組 1 (L1m 1)` で `g/A.go` と `g/B.go` が出る (除外が効かない。読み込みのエラーにもならない)
- levels を `["L1m"]` だけにした除外も読み込みでエラーにならない

## 発火条件

paths の除外に levels を明示し、L1 を含めて L1m を含めないとき (L1m を見る走査: `-levels …L1m` か sweep の節 2)。levels を省いた除外では起きない。

## 対応方針

1. `check` の粗い側の判定に「L1 を含むなら L1m も含める」を足す (L1m は L1 より粗く、L2・L3 とは比べられない段)。エラーの文言は今と同じ形
2. `elements` の段の並びに L1m を入れる (段の違う除外を新しく足された要素として出す)
3. 既存の設定で `levels` に L1 を書いて L1m を書いていないものは読み込みでエラーになる。dotfiles には `.dupscan.json` が無い。使っている repo (obaket ほか) の設定を数えてから入れる

## 受け入れ条件

- [ ] 再現をテストにして赤を確かめてから直す
- [ ] `elements` の L1m の有無が新しい要素に出ることをテストにする
