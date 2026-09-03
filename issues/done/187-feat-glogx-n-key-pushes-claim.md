# 187 feat: glogx の `n` (次にやる目印) で claim を commit + push できるようにする

> **却下 (2026-09-03)。** viewer に push 導線を足すのではなく、**未コミットの claim を hook で
> 拾って Claude がユーザーに伺う**形にした (`_claude/hooks/next-claim-uncommitted.sh`)。
> 下の本文は却下時点の設計案として残す。

起票日: 2026-09-02
重要度: P2
関連: [`_claude/rules/claim-issue-in-next-and-push.md`](../_claude/rules/claim-issue-in-next-and-push.md)
(claim の規範) / [`_claude/hooks/next-claim-push.sh`](../_claude/hooks/next-claim-push.sh) (Claude 側の
補助) / [docs/issues-viewer-spec.md](../docs/issues-viewer-spec.md) の「`n` で目印を付ける」節 /
issue 166 (予約の pane 表示。同じ「状態を見えるようにする」系)

## 何が欠けているか

2026-09-02 に「着手する issue は `issues/next/` へ移して claim し、**その移動だけを即 push する**」
という規律を入れた。複数マシンのセッションが同じ issue 列を処理するため、**push されていない
claim は他マシンから見えず、二重着手を防げない**ため (実例: 同日に別マシンと retro 164 の
切り出しを同時にやり、同趣旨のルール追記が衝突した)。

Claude 側は PostToolUse hook が「単独 commit して push したか」を注入する形で補助している。
しかし **人が glogx の `n` キーで目印を付ける経路は、この規律の外にある**:

- `n` は `issues_view.go:100` の `markNext` 確認を経て**実ファイルを `next/` へ移動する**
  (viewer で実ファイルを動かす唯一の操作)。移動するだけで **commit も push もしない**
- Go が直接 rename するので **Bash を通らず、hook も発火しない** (hook 冒頭に明記済み)
- つまり **claim の最も自然な瞬間 (人が「次はこれをやる」と決めた瞬間) が、いちばん
  他マシンへ伝わらない**

## やりたいこと

`n` で目印を付けた後、**その移動だけを commit して push する**導線を viewer 内に置く。

## 設計メモ (実装前に決めること)

### 前例がある — glogx は既に push / pull を持っている

`action_modal.go` が `git push` (`b`) と `git pull --rebase` (`u`) を **y/N 確認つき**で実行し、
実行中の状態 (`pushing`) とスピナーを持っている。**新しい能力ではなく既存の型に乗せる**話。

### 確認をどう出すか (3 案。要判断)

| 案 | 形 | 利点 / 難点 |
|---|---|---|
| A | `n` の確認モーダルに「commit + push もする」を含める (1 回の y/N) | 操作が 1 回で済む。**「目印だけ付けたい」ができなくなる** |
| B | `n` は今のまま。目印を付けた直後にトーストで「`b` で claim を push」と案内 | 既存キーに乗る。押し忘れる |
| C | `n` は今のまま + `N`? のような別キーで「claim を push」 | 明示的。キーが 1 つ増える (`N` は採番コピーで使用中) |

⚠️ **`n` は toggle** (目印を外す操作でもある)。外した側も push する必要がある
(claim の解除が他マシンへ伝わらないと、誰も着手していない issue が claim 済みに見え続ける)。

### commit は claim だけを含めること

`commit-with-pathspec.md` の規律どおり、**移動した 1 ファイルの旧パスと新パスだけ**を
pathspec に書く。viewer から叩く以上、作業ツリーには無関係な変更が載っている前提で組む
(混ぜると「push できない事情」に claim が巻き込まれる)。

### 失敗モードを先に決める

- **remote が進んでいる** → `git pull --rebase` が要る。viewer の `u` と同じ経路を使えるか
- **rebase が衝突した** → 規範は「claim の衝突 = 二重着手の証拠なので相手を尊重して退く」。
  viewer でそこまでやるのか、案内を出して人に委ねるのかを決める
- **push が失敗した** (ネットワーク / 認証) → 目印は既に付いている。ローカルとリモートで
  claim の状態が食い違うので、**トーストで「claim は push されていない」と明示する**
- **未コミットの変更が大量にある / 別の未 push commit がある** → claim だけ先に push すると
  他の commit も一緒に飛ぶ (push はブランチ単位)。**この点は規範側の想定と食い違うので、
  実装前に整理が要る** — 「claim だけ push」は技術的に不可能で、実際には
  「claim を積んでからブランチごと push」になる

⚠️ 最後の項目は規範 (`claim-issue-in-next-and-push.md`) の書き方の問題でもある。
「claim の移動だけを commit する」は正しいが、「その commit だけを push する」は
**ブランチに他の未 push commit があると成立しない**。実装時にどちらを直すか決める。

## 受け入れ条件

- [ ] `n` で目印を付けた後、viewer から離れずに claim を push できる
- [ ] 目印を外した側も push できる (toggle の両方向)
- [ ] claim の commit に無関係な変更が混ざらない (pathspec)
- [ ] push できなかったときに、その事実がトーストで分かる (無音で成功に見えない)
- [ ] `docs/issues-viewer-spec.md` のキー表と `src/glogx/README.md` を同じ変更で更新する
      ([`new-tool-requires-entrypoint-docs.md`](../_claude/rules/new-tool-requires-entrypoint-docs.md))
- [ ] 追加したテストは変異で red を確認する ([`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md))

## やらないこと

- **確認なしの自動 push** は採らない。push は外向きの操作で、glogx の既存の `b` / `u` も
  y/N 確認を持っている。ここだけ無確認にする理由が無い

## 却下の理由 (2026-09-03)

ユーザー判断: 「`n` に push を足すのは要らない。next への差分を Claude が見つけた時点で
push の可否を伺ってほしい」。

**この案の本質的な難点は本文の最後に自分で書いてあった**: 「claim の commit だけを push」は
push がブランチ単位なので成立しない。他に未 push の commit があれば一緒に飛ぶ。つまり
**`n` を押した瞬間に機械が push すると、飛ばしてよいか誰も判断していない push になる**。
飛ばしてよいかは人しか判断できないので、押した瞬間ではなく**伺う**形が正しい。

### 代わりに入れたもの

`_claude/hooks/next-claim-uncommitted.sh` (UserPromptSubmit)。毎プロンプトで作業ツリーを見て、
`issues/next/` の claim が未コミットなら「push してよいか」を Claude がユーザーへ伺う。

- 人が `n` で付けた claim (Go の rename なので Bash を通らない) もここで拾える
- 他に未 push の commit があれば「それも一緒に飛ぶ」ことを添えて聞く
- claim の解除 (next から出す方向) も拾う (本文が指摘していた toggle の両方向)
- 判定コストは実測 7ms。opt-in は既存の規律と同じ (`issues/next/` が実在する repo だけ)
- テスト 9 件 + 変異 4 本 red (`tests/claude/test_next_claim_uncommitted.sh`)

本文の受け入れ条件のうち「viewer から離れずに push できる」は**満たさない**。それが要ると
判断したときに再開する (このとき push のブランチ単位問題を先に解く必要がある)。
