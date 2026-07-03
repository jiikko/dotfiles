# Claude 会話のフォークを tmux popup で覗く（fork popup）

Claude Code の会話を `--fork-session` で枝分かれさせ、detached な tmux セッション
`claude-fork` として並走させたうえで、`C-t b` の popup でいつでも覗ける — という機構。
scratch popup（`C-t t`）の「フォーク版」として 2026-06-27〜28 に実装した。

**現在は bind を外して休眠中**。tmux クラッシュ切り分け（A/B 観測）で一時停止したのち、
「便利そうだが使いたいという気持ちにならなかった」というユーザー判断（2026-07-04）で
復活させないことにした。機構自体は動くので、必要になったら下記「復活手順」で戻せる。

## 使いにくかった実体（popup スタック問題）

実際に使っていた時の不満は、`display-popup` が**クライアント単位でモーダル**なこと。
scratch popup（C-t t）と fork popup（C-t b）を両方開くと popup の上に popup が
スタックし、main セッションに触るには LIFO で順に閉じるしかない。「フォークを
覗きつつ本線もすぐ触る」という並走用途と popup のモーダル性が根本的に噛み合わない。

復活させる場合は popup のまま作り直さず、構造を変えること:

- **通常 window 化**（`link-window` / `new-window` で claude-fork を開く）が本命。
  モーダルでなくなり window 切替で行き来できるため、スタック問題自体が消える
- popup の排他化（scratch/fork で popup スロットを 1 つに固定）は次点。
  スタックはしないが「両方同時に開く」は不可のまま
- side pane 化（join-pane で出し入れ）は却下済み: それは普段 prefix+| でやっている
  縦割り split と実質同じで、専用機構にする価値がない

## できること

1. Claude 会話中に `/fork-scratch` を実行すると、その会話が `--fork-session` で
   フォークされ、tmux セッション `claude-fork` に detached で起動する。
   元の会話はそのまま継続でき、フォークは別セッション ID なので競合しない
2. `C-t b` で `claude-fork` を popup（緑帯 🌿 FORK）として開き、フォーク側と対話できる
3. popup 内で再度 `C-t b` すると detach して閉じる（セッションは生きたまま）

ユースケース: 「この会話の続きで別の方針も試したい」「重い調査をフォークに投げて
本線は続ける」など、会話の分岐並走。

## 実装部品（すべてリポジトリに残っている）

| 部品 | 場所 | 状態 |
|---|---|---|
| フォーク作成コマンド | `_claude/commands/fork-scratch.md` | 冒頭に早期 exit ガードで無効化中 |
| popup 起動スクリプト | `scripts/tmux_fork_popup.sh` | 生きている（呼び出し元が無いだけ） |
| キーバインド | `_tmux.conf` の `bind b`（コメントアウト） | 無効化中 |
| テスト | `tests/tmux/test_fork_scratch.sh` | bind 依存の検査は skip、スクリプト不変条件は検査継続 |

## 設計上の要点（復活時に壊しやすい箇所）

- **空セッションは作らない**: fork は「claude を resume したセッション」でなければ
  無意味なので、`tmux_fork_popup.sh` は claude-fork 不在時に案内表示のみで閉じる。
  作成は `/fork-scratch`（Claude 会話側）の責務に一本化してある
- **`new-session -A` 禁止**: 既存セッションへの `-A` は「popup を閉じるのに 2 回押す」
  回帰を起こす（scratch で実測）。attach のみ
- **status-left の緑帯は session 単位の上書き**: scratch の点滅帯（global format 分岐）と
  機構が非対称なのは意図的。fork の帯は静的なので global の壊れやすい format 行に
  触れる必要がない
- 詳細な経緯（クラッシュ切り分けの顛末、孤児サーバ問題）は `_tmux.conf` の
  bind t / bind b 周辺コメントと `scripts/tmux_reap_orphan_servers.sh` を参照

## 復活手順

1. `_claude/commands/fork-scratch.md` 冒頭の早期 exit ガード（`echo ...; exit 0`）を削除
2. `_tmux.conf` の `bind b` のコメントアウトを外す。その際、scratch（bind t）と同様に
   開閉判定を `scripts/tmux_fork_popup.sh` 側へ集約する型に揃えること
   （scratch は 2026-07-04 にスクリプト集約型へリファクタ済み。bind は 1 行にする）
3. `make test-tmux` — bind b が復活すると `test_fork_scratch.sh` の A/B 検査が
   自動的に再有効化される
