# retro: claim が issue 本文から読めず、帰属の推測が 2 セッションで発生した (2026-09-11)

起票日: 2026-09-11
カテゴリ: retro / 対象セッション: dotfiles-4b（issue 360 の実装と、issue 358 の着手見送り）

## 何が起きたか

ユーザーから issue 358 を明示指示されて着手しようとしたところ、`issues/next/` に 358 / 359 の
claim symlink が既に push されていた。**issue 本文の 1 行目には担当者も状態も書かれていない**ため、
「誰が持っているか」「着手済みか、判断待ちか」がファイルからは読めず、生きている dotfiles
セッション 3 つ全てに `SendMessage` で照会する 1 往復が必要になった。

結果は dotfiles-53 が claim 主で、**実装は未着手（claim と手順提案のみ）**、`~/wt-358` は作成済み
（私の `git worktree add` が「already exists」で落ちたのはこれが理由）。ユーザーに 3 択を出して
「dotfiles-53 に任せる」が選ばれ、私は降りた。

照会の過程で dotfiles-d0 が「360 を done にした主体が 358/359 の claim 主である可能性が高い」と
commit の並びから推測して伝えてきたが、**これは誤り**（360 の claim `7d78a294` と done `c84e3c8f` は
私）。本人も後から「`commit-with-pathspec.md` の『推測した帰属を第三者へ伝えない』を、その条文を
引用しながら同じ message の中で破った。ヘッジを付けたことで自分を通してしまった」と訂正してきた。

## 気づき

### 1. claim バナーが本文に無いと、`next/` を見ない入口からは claim が存在しないのと同じ

`claim-issue-in-next-and-push.md` は既に「claim したら issue 本文の 1 行目にも担当者と状態を書く」
と要求しており、その理由（`next/` の目印は `next/` を見る入口にしか届かない）まで書いてある。
**ルールは在ったが運用で落ちた**: 358 にバナーが付いたのは `e5e3e578` で、**私の照会の後**。

バナーが先に在れば、照会 1 往復も dotfiles-d0 の推測も発生しなかった（私は「ユーザーが直接
指示したファイルを開く」という入口から来たので、`next/` を見る動機が無かった）。

🚨 **なぜ落ちたか（claim 主 dotfiles-53 の一次情報。本人から提供）**: ルール本文 1 行目の要求を
読んだうえで省いたのではなく、**`issues/next/` への push で「claim は完了」と認識して先へ進んだ**。
つまり発動点が「claim を push した直後」ではなく、**「push できた達成感の直後」**にある。
`next/` への push は hook（`next-claim-push.sh` / `next-claim-unshared.sh`）が促してくれるので
そこまでは到達するが、**その先のバナーには促す仕掛けが無い**ため、push した時点で claim の
タスクが閉じたように感じる。これは意志の問題ではなく、**強制手段が片側にしか無い**ことの帰結。

- 切り出し先の提案: **`claim-issue-in-next-and-push.md` への追記**（新規ルールは立てない。
  発動点が既存と同じ「claim する瞬間」）。追記する内容は規範ではなく**強制手段**の側 —
  現状のルールは「本文にも書く」と言うだけで、書き忘れを検出する仕掛けが無い。
  `next/` に symlink が在るのに対象 issue の 1 行目にバナーが無い状態を検出する検査
  （`tests/issues/` の既存の next 検査と同じ場所）を足せるか。
  🚨 起票するなら、その検査が名指しする退行（バナーを消す変異）で red になるまでを 1 セットにする

### 2. 「誰の claim か」を commit の並びから読もうとする力は強い

`commit-with-pathspec.md` は既に「author では区別できず、`git pull --rebase` が commit を挟むので
時刻の前後も根拠にならない。帰属に使えるのは commit が触ったファイルと、本人に聞くことだけ」と
書いている。dotfiles-d0 はその条文を**引用した上で**推測を伝えた。

ここで効いたのは「ヘッジを付ければ伝えてよい」という逃げ道で、**ルールが禁じているのは断定では
なく伝達そのもの**。本人の言葉を借りると「ヘッジは免罪にならない」。

- 切り出し先の提案: **却下**（新規の規範は不要）。ルール本文は既に正しく、破れたのは
  「ヘッジ付きなら伝達に当たらない」という読み替えの側。ただし同じ読み替えが繰り返されるなら
  `commit-with-pathspec.md` の当該項に「ヘッジを付けても伝達は伝達」の 1 行を足す価値がある。
  **1 回目なので今は却下**（2 回目が出たら追記する、を trigger にする）

### 3. 照会先を「生きているセッション全員」にしたのは効いた

`ListAgents` で見えた dotfiles セッション 3 つ全てに同じ照会を送ったところ、claim 主本人
（dotfiles-53）から一次情報が返り、残る 2 つからは「私ではない」が返った。
`parallel-write-agents-need-worktree-isolation.md` の「持ち主を推測して個別に聞くのではなく
全員へ送る」がそのまま効いた形。

- 切り出し先の提案: **却下**（既存ルールどおりに動いて期待どおりの結果が出ただけ。
  うまくいった話は retro に書かない、の対象）。**記録のみ**

## 残課題

- 2026-09-12 追記: **同じ形が再発し、1 往復で自己訂正された**（本 retro の主題の実例）。
  dotfiles-48 が「`issues/next/` の 359 は dotfiles-c9 の claim」と第三者へ伝えたが、
  根拠は「直前の連絡で 358/359 に触れていた」という**推測**で、git を見ていなかった。
  実際の claim は `eafada8a`（09-11 22:04「claim: issue 358 / 359 に着手」）で、
  claim 主は本 retro が記録している **dotfiles-53**（既に `ListAgents` に居ない）。
  訂正側が示した根拠は 3 つとも機械で確認できるもの（commit が触ったファイル /
  361 の一次情報の記載 / `ListAgents`）で、受け取った側も独立に裏を取って受け入れた。
  → [`commit-with-pathspec.md`](../_claude/rules/commit-with-pathspec.md) の
  「帰属に使えるのは **commit が触ったファイル**と、**本人に聞くこと**だけ」が
  そのまま効いた事例。**規範は既にあり、落ちたのは運用**（本 retro の「ルールは在ったが
  運用で落ちた」と同じ構造）。新規ルールは要らない
- 2026-09-12 追記: 採番側の姉妹形（「事前の合意は採番の証明にならない」）は
  [367](367-retro-adversarial-review-rounds-5-7-2026-09-12.md) に一本化した。
  本 retro の残課題と**まとめて判断できる**
- 2026-09-12 追記: 見送った先の **[issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md)
  は敵対レビュー 5 周目まで完了して決着した** (dotfiles-c9)。本 retro の残課題はこれとは独立で、
  未解消のまま

- [ ] 気づき 1: `claim-issue-in-next-and-push.md` への追記 + バナー欠落の検査を issue 化するか
      （ユーザー判断待ち）
- 気づき 2: 却下（1 回目のため。2 回目が出たら `commit-with-pathspec.md` へ 1 行追記）
- 気づき 3: 却下（既存ルールが期待どおり効いた記録のみ）

## 関連

- [issue 360](done/360-test-tmux-shim-value-taking-options-completeness.md) — 同セッションでやり切った実作業
- [issue 358](done/358-refactor-lockman-cleanup-selftoken-is-production-unreachable.md) / [issue 359](359-research-lockman-resource-leaks-perf-audit-2026-09-11.md) — 着手を見送った先（dotfiles-53 が担当）
