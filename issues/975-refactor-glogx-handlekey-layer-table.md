# 975 (refactor): glogx の handleKey (253 行) の順序つきの if の連鎖を、層の表にするか (結論: 表にせず、順序のテストを足す)

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

## 設計のレビューの決着 (opus、読み取りのみ)

- 「表にしない」「後半を routeKeyToLog へ出す」の挙動は壊せなかった。ただし 3 問への答えは「routeKeyToLog は名前を付けて移す以上の効果が小さい」:
  issue の動機の段の順序は handleKey に残ったままで、U は handleKey・Dismiss は routeKeyToLog と分かれて他の routeKeyTo* と形が揃わない
  → **routeKeyToLog への切り出しは採らない** (`_claude/rules/verify-design-intent-before-refactor.md`: どれも良くならないなら分けない)
- P2 段の順序 (action モーダル → 再起動ダイアログ → 全画面) の写しが onPaste にもある → `boardOwnsInput` 1 つにまとめた (下)
- P3 根拠に挙げた fix (879ded3f2・132cc8123) は述語の追加が中心で「置き場所の誤り」だけではない (結論の「表にしても置き場所は人が選ぶ」は崩れない)
- 推測で挙がった「入れ替えても緑」の組は、下の変異で実測した

## 決めたこと

- handleKey は層の表にせず、後半も切り出さない。代わりに、段の優先を守るテストを足し、onPaste の板の判定を 1 つにする

## 進捗

- [x] 着手の判断 (2026-10-29 再開。上の「調べたこと」で見送らないと決めた)
- [x] 隣り合う段を入れ替える変異を 11 組当てた (意味のある組だけ。受けるキーが互いに別の段 (b と u など) の入れ替えは等価なので外した)。
  既存のテストで red: C-c・C-g ↔ action モーダル / action モーダル ↔ prefix / update ↔ 全画面 / 全画面 ↔ U / q ↔ job パネル / job パネル ↔ 一覧の switch。
  緑のまま通った 5 組の扱い:
  - 再起動ダイアログ ↔ C / X の update → TestRestartPromptTakesUpdateKeys を足した
  - usage の消去 ↔ diff の重ね板 → TestKeyOverDiffDismissesUsage を足した
  - PR status ↔ i → TestPRStatusTakesIssuesKey を足した
  - キーリピートの抑止 ↔ 終了演出の着地 → 足さない (押しっぱなしのキーが演出の途中で着地するかの差だけ)
  - diff ↔ PR status → 足さない (片方が開いている間はもう片方を開くキー d / P が届かず、同時に開く経路が無い = 等価な変異)
  足した 3 本は、入れ替えた変異でそれぞれ red になることを確かめた
- [x] onPaste: 全画面ごとに 3 回写していた `!actModal.active() && !restartPromptVisible()` を `boardOwnsInput` にまとめ、switch 1 本にした。
  早期 return を外す変異で TestPasteGoesOnlyIntoInputs が red
  — commit「test(glogx): handleKey の段の優先を守るテストを足し、onPaste の板の判定を 1 つにする (issue 975)」
- [x] 実装の敵対的レビュー (opus、読み取りのみ): P1・P2 なし。onPaste はド・モルガンで旧と同じ条件・全画面の分岐は排他。
  足した 3 本は、前提を立てた上で主張する優先だけで red になる (別の機構で弾かれていない)。「diff と PR status が同時に開く経路は無い」も
  開く経路 4 つを辿って正しい。P3 (boardOwnsInput の doc の「全画面への振り分けより前」の間に C / X の段も挟まる) は記述として誤りでないので直さない
- [x] ルートの make test rc=0 (2026-10-30)。push 済み (cd0fc2ea2)。CI は Issues / Lint / unused / src/glogx / src/doctor / doctor / dupscan gate が緑
- [ ] CI の Tests と Bench: cd0fc2ea2 から後の 8 commit の run がどれも後続の push で取り消された (concurrency の cancel-in-progress。
  再実行も同じ group で取り消された)。私の commit を含む commit で完走した run を見てから done にする
