# 164 retro: doctor ③ の実装と敵対的レビュー 2 周 (2026-09-02 後半)

起票日: 2026-09-02

## このセッションでやったこと

1. issue 148 ③: glogx の doctor 画面 (D) + キャッシュ + 起動時トースト (`cb832dc`)、案 A レイアウト + カーソル + Enter 展開 (`824b863`)、
   y/Y コピー + 重いエントリの再利用 (`cd80c37`)、5 分 snapshot (`d229a96`)、🚨 への置換 (`bec2b16`)
2. 共有 module 化 (`f44b6c3`) とその敵対的レビュー修正 (`30a5956`: replace 先の再ビルド漏れ / WaitDelay / 外部 spawn)
3. 敵対的レビュー 2 周目 (判定ロジック + 画面) の P1 4 件を修正 (`824b863`)
4. issue 163 (次の枠で走らせる red team 手順書、6 観点) を起票

## 反省・気づき (切り出し先を提案。実行はユーザーの判断待ち)

### 1. サブエージェント 3 体が session limit で同時に死んだ

5h 枠の残量を見ずに 3 体を起動し、3 体とも途中で 429 で落ちた (成果ゼロ、再起動で二重コスト)。
**切り出し先案**: `subagent-model-tiering.md` に 1 行 (「枠の残量が少ないときは並列数を絞る / 枠が開いた直後に起動する」)。
issue 163 の「走らせ方」には書いた

### 2. 相対パスの pathspec で commit が空振り (2 回)

`cd src/glogx` した状態で `git add src/doctor/...` / `git commit -- src/...` を打ち、pathspec が解決できず commit されないまま
「push 済み」の確認だけが通った (hook の git state 表示で気づいた)。**切り出し済み** (2026-09-02): `commit-with-pathspec.md` に「pathspec は cwd 相対で解決される — repo root へ
移動してから打つ」節を追加。実測を裏取りして書いた: 空振りは**無音ではなく** `git commit` は rc=1 +
`error: pathspec ... did not match any file(s) known to git`、`git add` は rc=128 を返す。
誤認は次の `git push` が **`Everything up-to-date` で rc=0** を返すところで起きる。
だから対策は「エラーを見る」ではなく cwd の固定。実例は rationale へ

### 3. 「⚠️ の表示幅が揺れる」は実端末で見ないと分からなかった

サンプルは cat で見ていたが揺れは動画的な現象で、ユーザーが動作確認して初めて出た。`decide-layout-in-sample-renderer-first.md` の
「出して見る」は静止画では足りない場面がある。**切り出し先**: `no-mixed-width-columns-in-terminal-ui.md` に追記済み (規約化)

### 4. 「高頻度 toggle で毎回スキャン」「I/O コスト」はユーザーが動かして初めて出た要件

設計時点で「開いた時にスキャン」を要件どおり実装したが、popup 運用 (C-g で開閉) の頻度を考えると当然の要件だった。
snapshot 5 分 / 重いエントリ 1 時間で吸収。**切り出し先案**: 却下 (issue 148 の 3 章「スキャンの三層構造」に書けば足りる。
次に同じ器で診断項目を足す人が読む場所はそこ)

### 5. UI 側の「診断できず」表示に 1 本もテストが無かった

CLI 側 (Format) はテストがあり、UI は同じ分岐を別実装していて無防備だった (敵対レビュー P1)。同じ結論を 2 箇所で描く形は
片方だけ守られる。**切り出し済み** (2026-09-02): `mutation-verify-new-tests.md` の「守っていないテストの形」に
「同じ判定・同じ結論を 2 箇所で別実装していないか。テストがある側だけ見て安心していないか」を 1 項追加
(両方へ同じ入力を流して突き合わせる / 根治は出典の一本化)。却下にしなかったのは、体 6 の指摘で
**同じ形が 2 回目として出た**ため (issue 179: svc の注記が CLI にしか無く、逆に system ドメインの注記は
UI にしか無かった = 欠落が両方向)。実例は rationale へ

## 残課題

- [x] 項目 1 → **採用 (2026-09-02)**。[`subagent-model-tiering.md`](../_claude/rules/subagent-model-tiering.md)
      に「枠の残量が少ないときは並列数を絞る。残量を見ずに複数体を起動すると全体が 429 で死ぬ」を追加
      (3 体同時起動で 3 体とも途中終了、成果ゼロ。同日さらに 6 体並行で全滅した実例も出た)
- [x] 項目 2 → **採用 (2026-09-02)**。[`commit-with-pathspec.md`](../_claude/rules/commit-with-pathspec.md)
      に「pathspec は cwd 相対 — repo root へ移動してから打つ」節を追加。実測つき
      (root 相対を渡すと `git commit` は rc=1 / `git add` は rc=128 で **commit されない**。
      その後の `git push` は `Everything up-to-date` の rc=0 を返すので push の出力では気づけない。
      パイプ越しに叩くと rc そのものを見失う)
- [x] 項目 5 → **採用 (2026-09-02、別マシンのセッションが実施 `6274192a`。2026-09-02 に本人確認)**。
      [`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) に 1 項追加。
      ⚠️ 本セッションは同じ項目を**却下する**方向で検討していた
      (「同じ間違いが別の場所にもある前提で grep する」が CLAUDE.md の一般則として既にあるため)。
      別セッションの採用が先に入っていたので、**却下へ差し戻さない** —
      既に入った規範を外すには [`list-masked-failure-modes-before-removing-guard.md`](../_claude/rules/list-masked-failure-modes-before-removing-guard.md)
      の手順 (何をマスクしていたかの列挙) が要る。重複だと感じたら、そのときに正式に外す

残課題なし。
