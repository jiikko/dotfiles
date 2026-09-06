# 185 retro: プロンプト監査 (issue 165) の適用 15 hunk (2026-09-02 夜)

起票日: 2026-09-02

## このセッションでやったこと

issue 165 (`/claude-api prompt-audit` の結果) の未適用分をユーザー指示で 5 件ずつ 3 回に分けて適用し、
High 8 / Medium 13 を全件決着させて `issues/done/` へ送った。

1. H4/H5 (`f1bf7e4`): fable SKILL.md の memory 再掲をやめ委譲、仕様節を Fable 5.1 へ
2. H6/H7/H8 (`f743df7`): agents の英語テンプレ定型 ("am I being lazy?" / "Surface-level X is insufficient" / 同文反復)
3. M1/M2 (`dfc2ff2`): モデル名ピンを外す、レビュワーを「外部レビュー」へ一般化
4. M3/M13 (`cc7d510`): rules 本文の事故ナラティブ 7 箇所を rationale へ、rationale 2 本を新設
5. M4/M5 (`ed4e927`): codex-drive の実測記述は現状維持を明記、forge の出力例に wire format の断り
6. M7 (`1ad3f18`) / M8 (`2f323d5`) / M10・M11 (`c7610b5`) / M12 (`a5950ff`)
7. M9/L2 (`3f1f9bc`): **提案とは逆向きの対応** (下記 1)
8. 並行して issue 184 (tmux 入力予約の Enter が効かない件) を起票

## 反省・気づき (切り出し先を提案。実行はユーザーの判断待ち)

### 1. 監査レポートの提案を、前提を実測せずに当てると壊す方向へ進む

M9 は「共通スキャフォールドを `_common/` へ 1 部にまとめる」提案だったが、その前提
「agent 定義の `@../_common/...` 参照が展開されるか」は issue 自身が L2 で**未確認**と書いていた。
実測したら**展開されない** (`architecture-reviewer` に自分の instructions を引用させると
`See @../_common/language-adaptation.md for guidelines.` の 1 行がリテラルで届いていた)。
提案どおり移していたら、読まれない場所へ規約を移すことになっていた。

しかも実測の副産物として、**2 agent の Language Adaptation がこれまで一切効いていなかった**
既存バグが出た (L2 が「参照している 2 本の方が壊れている」と書いていた側が正しかった)。

**切り出し先案**: `verify-design-intent-before-refactor.md` に 1 行
(「提案が『共通化して 1 箇所にする』型のとき、**参照機構が実際に展開されるか**を移す前に実測する。
展開されない参照へ移すのは、削除と同じ」)。あるいは移送先を `move-report-conclusions-to-issues.md`
の「issue 本文の未確認事項は着手時に実測する」側にする案もある。判断はユーザーに委ねる

### 2. 「監査が出した diff」を hunk 単位で当てる運用は機能した

15 hunk を 1 hunk = 1 commit 前後で当て、そのたびに issue の適用ログへ commit hash と
「提案から変えた判断」を書き戻した。結果として、**却下 3 件・現状維持 1 件・逆向き対応 1 件**が
理由つきで残り、次の監査が同じ指摘を再生成しても即棄却できる状態になった。
**切り出し先案**: 却下 (`move-report-conclusions-to-issues.md` が既に規範として持っている。
今回はそれが機能した実例なので、rationale へ 3 行足すだけで足りる)

### 3. 「7 本新設」のような件数指示を鵜呑みにしなかった

M13 は「rationale が欠落している 7 本を新設」だったが、実際に移す実例があったのは 2 本だけで、
残る 5 本は**本文に移すべき実例が無く、dangling な参照も無い** (実測: 全 rules で
`rules-rationale/<同名>` を参照しつつファイルが無いものは 0 件)。空ファイルを 5 本置くのは
`claude-md-maintenance.md` の「とりあえず置く」禁止に反するので作らなかった。
**切り出し先案**: 却下 (既存ルールで判断できた。ルール追加は不要)

### 4. `make test` を 1 度も完走できていない

2 分でタイムアウトし、以降は link 完全性テスト単体しか回していない。今回の変更は散文と
rationale 2 本の追加だけなので影響は薄いと見ているが、**全体テストで裏を取っていないのは事実**。
**切り出し先案**: 新規 issue —「`make test` の所要時間を測り、長いなら分割 target
(`make test-claude` 等) を用意する」。今回のような docs 変更で全体テストを回せない状態は、
「回さない習慣」を作る

### 5. hook の偽陽性で heredoc が 1 回 deny された

`deny-bare-tmux-kill.sh` が、docs へ移すコマンド本体の文字列 (引用符に入らない heredoc 本文) を
検出して deny した。既知の罠 (`tmux-probe-requires-socket-isolation.md` の「強制手段」節に
記載あり) で、scratchpad のスクリプト経由に切り替えて回避した。
**切り出し先案**: 却下 (ルールに既に書いてある。今回は書いてあるとおりに回避できた)

## 残課題

- [x] 項目 1 → **採用 (2026-09-02)**。[`verify-design-intent-before-refactor.md`](../../_claude/rules/verify-design-intent-before-refactor.md)
      に「『共通部を 1 箇所へ集める』提案は、移す前にその参照機構が実際に展開されるかを実測する。
      展開されない参照先へ移すのは削除と同じで、ファイルは在るので気づけない」を追加。
      別ルールにはしなかった: 発動点が「共通化しようとした瞬間」で、あちらの
      「refactor を提案する前に確認する」チェックリストと同じ位置にある
- [x] 項目 4 → **issue 化した**: [issue 188](188-test-make-test-duration-and-split.md)
      (`make test` の所要時間を測り、分割 target を用意するか判断する)
- [x] 未 push の commit → 2026-09-02 に push 済み

残課題なし。
