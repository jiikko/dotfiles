# 975 (refactor): glogx の handleKey (253 行) の順序つきの if の連鎖を、層の表にするか

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

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

## 調べたこと (2026-10-29)

- 効果の見積もり: 直近 90 日に tui.go / browse_keys.go を触った 181 commit のうち、handleKey に hunk を持つのは 66 本、
  そのうちトップレベルの if / switch を足したのは 18 本 (新しい全画面 F / L / D / R / s・板・キーリピートなど)。順序の取り違えを直した fix も
  ある (879ded3f2 再起動ダイアログと action モーダル・132cc8123 演出中の e)。見送るほど小さくはない
- handleKey はコメントを除くと約 140 行。段は上から: キーリピートの抑止 / 終了演出の着地 / 閉じる演出の着地 (issues・status。e だけ捨てる) /
  C-c・C-g / action モーダル / tmux の prefix の案内 / キーの書き換え (ctrl+f → right、Space の表記) / 再起動ダイアログ / C・X の update /
  全画面への振り分け (switch) / 全画面を開くキー (U R D F L) / ここから下は git log の一覧: usage の消去・diff・PR status の重ね板・
  i・s・b・u・w・q・job パネル・一覧のキー

## 設計 (案)

- **層の表 (`[]struct{accepts, handle}`) にはしない**。途中でキーを書き換える段 (ctrl+f・Space) と段の間の副作用 (FinishClose・usageOv.Dismiss)
  を表の外の前処理に持つことになり、入口が 2 つに割れる。順序の取り違えの fix はどれも「どこに置くか」の誤りで、表にしても置き場所は
  人が選ぶので防げない
- **git log の一覧のキー (usageOv.Dismiss 以降) を `routeKeyToLog(key)` へ出す**。他の全画面が `routeKeyTo<Screen>` を持つのと同じ形で、
  handleKey は「全体の前処理 → 全画面への振り分け → 全画面を開くキー → 基本の画面 (一覧) へ」だけになる。置き場所は browse_keys.go のまま
  (一覧は全画面ではないので browse_screens.go に置かない)。本体は書き換えずに移す
- **順序のテストの穴を埋める**: 今のコードに、隣り合う段を入れ替える変異を当て、緑のまま通る順序にテストを足す
  (`_claude/rules-checklists/mutation-verify-new-tests.md` の「if の連鎖・switch の暗黙の順序」)。切り出しの前に足し、切り出しの後も同じ変異で赤を確かめる

## 関連

- [971](done/971-refactor-glogx-tui-split-by-responsibility.md) (tui.go の分割)
- 863 の P1〜P4 (mac-lan-manager の lanmgr で、行の動作と画面の層を表にした前例)

## 進捗

- [ ] 着手の判断 (trigger 待ち)
