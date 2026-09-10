# retro: 本番 tmux 誤殺の調査と二層 kill ガードの構築 (2026-09-11)

起票日: 2026-09-11
カテゴリ: retro

## このセッションでやったこと

ユーザーの「tmux server exited を調査して」から始まり、本番 tmux (default, 30 セッション) が
Claude 起因で kill された事故を特定し、恒久対策の二層ガードを作った。

| commit | 内容 |
|---|---|
| 調査 | 犯人 = 別セッション dotfiles-4b の敵対レビュー用サブエージェント。経路: 存在しない TMPDIR → `mktemp -d` 失敗 → `TMUX_TMPDIR=""` → tmux が /tmp へフォールバック → `tmux -L default kill-server` が本番直撃。証拠は `~/.cache/tt-restore-trigger.log` の `kill-cmd ... issuer=` 行 (watchdog の親子連鎖) |
| `7ea15f96` | 二層ガード新設: `bin/tmux` shim (PATH 先頭で script 内部の tmux も傍受) + deny hook の `-L default` 拒否追加 |
| `7ee9a9b0` | P1 大文字綴り (`-L DEFAULT`) / P2 pty の TTY gate 越え |
| `2cb8c4b3` | P1 `;` 連鎖の後続 kill 見落とし / kill-session -a 昇格 |
| `a77c4163` | 密着末尾 `;` (`kill-server;`) |
| `b644bd70` | **round-4 (a77c4163) 自身が作った** 空白付き `;` 区切りの見落とし (`display -p 'a ;' kill-server`) |
| `61ccc88a` | 最終ゲート通過。load-bearing 前提を doc へ |
| docs | `docs/tmux-production-kill-guard.md` 新設 + 索引 |

Opus の敵対的レビューを 6 周。各周が実害を出し、最終周で「壊せなかった」。全工程で本番 30 セッション無傷
(decoy + stub で検証)。

## 気づき

### 1. 🚨 安全機構の敵対レビューは「修正差分」にも回す — 自分の fix がバグを作った実証

`adversarial-review-own-safeguards.md` §7「修正した差分にもう 1 周回してから閉じる」を、このセッションが
**強く実証**した。round-4 (`a77c4163`) で私が入れた「内部空白のある token はスキップ」という正規化が、
`display -p 'a ;' kill-server` (非 kill の引数が空白+`;` で終わり連鎖を区切る) を見逃す catastrophic な
under-block を**新設**していた。round-5 のレビューが decoy で実測して発見。私は round-4 の時点で
「内部空白 → tmux は終端にしない」と decoy で確認したつもりだったが、`kill-server ;` が ALIVE な理由を
「区切らないから」と誤読していた (実際は `kill-server ` が未知コマンドだから)。

**切り出し先の提案**: `rules-rationale/adversarial-review-own-safeguards.md` の §7 節に実測例として 1 つ
(「6 周・各周が実害・4 周目の fix 自身が同クラスの穴を作った」)。本文 (rules/) は既に §7 が規範を持つので
追記不要。→ ユーザー判断待ち。

### 2. 文字列検査 hook は script 内部の呼び出しを構造的に見られない — PATH shim が唯一の層

2026-07-30 の後に deny hook を入れたのに 2026-09-11 で再発したのは、kill が `bash <script>.sh` の内部で
起きコマンド文字列に現れなかったから。恒久対策は「実行時に tmux 呼び出しそのものを傍受する」PATH shim。
→ 既に `tmux-probe-requires-socket-isolation.md` の強制手段節と doc に記載済み。切り出し不要。

### 3. TTY を「意図的ユーザー」の proxy にするのは pty で破れる

エスケープハッチを TTY で閉じたが、pty (script/expect/tmux ペイン内。この repo は対話テストで pty 必須) が
チェーンに入ると非対話でも TTY が立つ。catastrophic な kill-server は TTY 無視で無条件 block にし、
個別 kill-session だけ TTY 許可を残した。→ doc の残存リスクに記載済み。切り出し不要。

### 4. 破壊的ガードのテストは decoy + stub で本番を触らず検証できた

決定 (block/pass) は REAL をスタブに差し替えたコピーで、実 exec の証明は隔離デコイ (ユニーク -L + 短い
TMUX_TMPDIR) で。6 周・多数の変異を通して本番に一度も触れなかった。`sandbox-real-destructive-test-apis.md`
の実践例。レビュー側も「隔離環境の失敗は本番の失敗ではない」(A-B 状態共有の汚染で `kill-server ;` を
一度 DEAD と誤測 → 隔離し直して ALIVE 確定) を踏んでおり、`mutation-verify-new-tests.md` の A-B 隔離が効いた。
→ 切り出し不要 (既存ルールの実践)。

### 5. active threat + ユーザーの緊急要求下では「有効化してからレビュー」を選んだ

各 fix を、レビュー完了を待たずに push+pull で有効化した。理由: ライブの shim に実際の穴があり、
threat (4b が同じテストを再実行しうる) が active で、ユーザーが「絶対に消せない」「本当に仕事終わる」と
緊急だったため。fail-open の最悪ケースは「今と同じ無防備」なので、十分自己検証した fix を先に当てる方が
安全側。レビュー指摘は follow-up で当てた。→ 判断の記録。ルール化はしない (状況依存)。

## 残タスク

（2026-09-11 に dotfiles-82 が処理。残タスクなし。実装側は dotfiles-6f が完了済み）

- [x] 気づき 1 の切り出し → `rules-rationale/adversarial-review-own-safeguards.md` に
      「実測 11〜12 回目」として追記した。**4 周目の fix 自身が catastrophic な under-block を
      新設した**ことを、§7 の最も強い形（指摘への修正は新しい安全機構であり、同じクラスの穴を
      持ちうる）として記録。`rules/` 本文は §7 が既に規範を持つので追記していない
- [x] （任意）値取りグローバルオプション集合 `{c,f,L,S,T}` の完全性チェック →
      **[issue 360](../360-test-tmux-shim-value-taking-options-completeness.md) として起票**。
      `bin/tmux` は変更せず、usage と shim の集合を突き合わせる検査を新設する形にした
      （dotfiles-6f と重複しないよう、起票はこちらが引き取ることを合意済み）
- 上記以外の実装・検証・doc・本番有効化は完了済み
