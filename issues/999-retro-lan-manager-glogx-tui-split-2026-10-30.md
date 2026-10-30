# 999 (retro): lan-manager の 847・964・967 と glogx の tui.go の分割 (971・975) の振り返り

起票日: 2026-10-30

## 概要

2026-10-29〜30 の 1 セッションで、847 (lan-manager の疎通) を閉じるための 863 P7 の寄せ・964 (有線の名前の広告をループにする)・
967 (4 台の入れ直し)・968 (retro の提案の追記)・971 (glogx の tui.go を責務ごとの browse_*.go に分割)・975 (handleKey の段の優先のテスト) を行った。
踏んだ所と回りくどかった所を書く。

## どこで踏んだか

### 1. 同じ分割を以前に実測して見送った記録を、設計を書く前に見つけられなかった (971)

- tui.go の browseModel の型の直前に「塊ごとに分けても tui.go に触らずに済む commit は 2〜3 割。再提起するなら同じ方法で数え直すこと」という
  見送りのコメントがあった。設計の反証レビュー (opus) が見つけるまで、私は読んでいなかった
- verify-design-intent-before-refactor の 1 は「CLAUDE.md / README / docstring」を読めと言う。私は glogx の CLAUDE.md・README・ratchet の
  テストは読んだが、対象ファイルの中の「見送った」「採らない」のコメントは探さなかった (4241 行のうち、型の宣言の上にあった)
- 数え直した結果 (39〜67%) で進めたので実害は無かったが、レビューが見つけなければ前の判断を知らずに逆転していた

### 2. CI の Tests と Bench が、push の多い時間帯に何時間も完走しなかった (971・975)

- Tests / Bench の concurrency は `cancel-in-progress: true` で、dotfiles には数分おきに別のセッションが push する。
  975 では cd0fc2ea2 から後の 8 commit の run がどれも取り消され、再実行も同じ group で即取り消された。完走を見るまで約 2 時間かかった
- 既存の規則 (verify-execution の「取り消された run は、それを含む後の commit の run で確かめる」) どおり祖先で探して見つけたが、
  push が途切れない限り、どの commit の run も完走しない形は規則では解けない (CI の設計の問題)

### 3. 局所 (直したか / 直さないかだけ)

- 分割の道具 (go/parser で位置を取って切り貼り) は、等価の証拠を「移す前と後の行を並べ替えて突き合わせる」で取れた。手で写すより速く、
  レビューも行の対応で確かめられた。使い捨てで終わり (tmp/971-2026-10-29/tool)
- 否定の確認 (起こしていない) を 3 周で見て、偽の dns-sd の起動の遅れで変異を見逃した。変異で気づいて 20 周に広げた (規則どおり変異で捕まえた)
- ① の commit message に「本体の最後が return で無い case にだけ `return m, nil` を足した」と書いたが、該当は 0 件だった (道具の
  動きから書き、出力で数えなかった)。実装レビューで分かり、issue 971 に訂正を書いた
- 足したコメントに issue 番号を書き、ルートの make test の test-comment-provenance で落ちた (module の test / lint では出ない)。直した

## 提案

- **P-1 (1 から)**: `_claude/rules/verify-design-intent-before-refactor.md` の確認の 1 に追記する。案の文: 「対象のファイルの中も
  『見送った』『採らない』『再提起するなら』の語で grep する (判断の記録は型や関数の直上のコメントにあることがある)」。
  - 切り出し先: 既存ルールへの追記。発動点 (分解を提案・設計する前) が同じ
- **P-2 (2 から)**: 新規 issue。dotfiles の CI の Tests / Bench が、push の多い時間帯に `cancel-in-progress` で完走しない。
  master では一定の間隔で 1 本は完走させる (取り消さない run を周期で起こす・取り消しを master では切るなど) を検討する。
  - 切り出し先: 新規 issue (CI の設計の判断が要る。規則では解けない)
- **却下: 3 の局所 4 件**。どれも直したか、既存の規則 (変異で確かめる・主張は出力で数える・ルートの gate を回す) で捕まえた形。
  切り貼りの道具は 2 回目に要るまで汎用化しない (new-tool-requires-entrypoint-docs の「使い捨て」)

## 進捗

- [x] P-1 (ユーザー指示「両方やって」2026-10-30): `_claude/rules/verify-design-intent-before-refactor.md` の確認の 1 に追記した
- [x] P-2 (同): [1000](1000-chore-ci-tests-bench-never-complete-under-push-traffic.md) を起票した
- 1000 を採番するときに、採番の手順・番号の重複の検査・issue_done.sh が 3 桁を前提にしていると分かった → [1001](1001-bug-issue-tooling-assumes-three-digit-numbers.md)
