# 975 (refactor): glogx の handleKey (253 行) の順序つきの if の連鎖を、層の表にするか

起票日: 2026-10-29

> 2026-10-29 に pending から再開した (ユーザー指示「リセット後にやって」)。

## 概要

`src/glogx/browse_keys.go` の `handleKey` は、押されたキーをどの層が先に受けるかを、トップレベルの if / switch 25 個の
並び順で決める (2026-10-29 時点)。順序の制約は「🚨 〜より前に置くこと」の形でコメントに 14 か所あり、
コンパイラもテストも並びの全体は守っていない (hint_registry_gate_test が一部の順序を見る)。

971 で update を「メッセージ → 受け手」の表にしたのと同じく、handleKey を「層 (受けるかの述語・処理) の並び」の
表にすると、順序が 1 か所に並んで見える。

## 着手するときにやること

- 設計の前に、各層の「受けるかの判定」と「受けたときの副作用」と「先に効く層」を表にする (`_claude/rules/survey-receiver-guards-before-passing-new-values.md`
  の「複数の読み手の判定を 1 つの述語・表に寄せるとき」)
- 表へ移したら、隣り合う 2 つの層を入れ替える変異を当てて、優先を守るテストがあるかを確かめる
  (`_claude/rules-checklists/mutation-verify-new-tests.md` の「if の連鎖・switch の暗黙の順序を表や関数に移したら」)。無ければ先に足す
- 効果は、直近 90 日の handleKey の hunk のうち「新しい層を足す変更」の数で見積もる (971 と同じ数え方)。
  足す変更が少ないなら、表にしても得るものが小さいので見送る

## 関連

- [971](done/971-refactor-glogx-tui-split-by-responsibility.md) (tui.go の分割)
- 863 の P1〜P4 (mac-lan-manager の lanmgr で、行の動作と画面の層を表にした前例)

## 進捗

- [ ] 着手の判断 (trigger 待ち)
