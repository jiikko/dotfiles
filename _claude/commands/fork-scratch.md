---
description: 今の会話を --fork-session でフォークし detached tmux セッション claude-fork に作成（C-t b の popup で覗ける）
allowed-tools: Bash(tmux:*)
---

今の Claude 会話を `--fork-session` でフォークし、detached な tmux セッション `claude-fork` として起動する。元の会話（このセッション）はそのまま継続でき、フォークは新しいセッション ID に枝分かれするので競合しない。

> ⚠️ **休眠中（復活させない判断済み）**
> 発端は 2026-06-28 の tmux クラッシュ切り分け（popup 機構の A/B）だが、その観測は
> 2026-07-04 に終わっている。現在このコマンドが止まっている理由は「便利そうだが使いたいという
> 気持ちにならなかった」という同日のユーザー判断で、**復活させないことが決まっている**。
> 実行されるのは案内の echo だけで、フォーク作成の本体は docs 側へ移してある。
> 復活させる場合は popup のまま戻さず通常 window 化を検討すること（popup はクライアント単位で
> モーダルなためスタック問題が残る）。判断の正本・復活手順・設計上の注意は
> `docs/claude-fork-popup.md`。

次を実行し、その出力をユーザーに伝える:

```bash
echo "/fork-scratch は一時無効化中です (休眠。理由と復活手順は docs/claude-fork-popup.md)。"
```

復活させるときの手順（フォーク作成コマンドの本体・`C-t b` の popup・失敗時の点検）は
`docs/claude-fork-popup.md` を正本にする。
