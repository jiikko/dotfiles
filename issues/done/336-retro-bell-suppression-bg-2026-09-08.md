# retro: bg 待機中のベル抑制と notification_type denylist の実バグ (2026-09-08)

依頼: 「tmux で Claude Code が入力待ちになったらウィンドウのアイコンをベルマークにしているが、
バックグラウンドで実行しているなら入力待ちでもベルマークを出さないでほしい。調べて」
+ 途中で「ついでに実バグも修正して」。

対応 commit: `fix(hooks): bg 待機中は入力待ちのベルを出さない / notification_type の denylist を実在の値へ直す`
/ `docs(README): 状態表に ⚙ working (bg:N) を足し、bg 待機中は鳴らないことを書く`

## やったこと

- [x] Notification hook (input) が bg 待機中はベル・macOS 通知・`🔔` 表示を出さないようにした
- [x] `@claude_bg` を Stop が書き、input が読む形で bg の有無を渡す
- [x] `notification_type` の denylist を claude 2.1.263 の enum と突き合わせて直した
- [x] テスト追加 (Test 5 / 6 / 6b / 6c)。変異 5 本で狙ったケースの red を確認
- [x] README の状態表に `⚙ working (bg:N)` の行を追加

実測:
- 変異 M1 (bg 抑制を外す) → Test 6b の 3 ケースが red
- 変異 M2 (working で bg フラグを落とさない) → Test 6c の 1 ケースが red
- 変異 M3 (bg なし Stop でクリアしない) → Test 6c の state ケースが red
- 変異 M4 (denylist から push_notification を外す) / M5 (denylist に permission_prompt を足す)
  → それぞれ該当ケースのみ red

## 気づき

1. **「入力待ち」は 1 つの状態ではなかった。** `notification_type` は 14 値あり、
   「人が答えないと前へ進まない」6 値と「情報通知」7 値が混ざっている。この区別を持たずに
   denylist を書いたため、**実在しない値 2 つ (`elicitation_complete` / `elicitation_response`)
   が並び、何も除外していなかった**。実在するのは `elicitation_dialog` /
   `elicitation_url_dialog` で、どちらも入力が要る側。
   → 切り出し先: **対応済み** (hook のコメントに「新しい値を足すときは enum を引き直す」と
   引き方つきで残した)。ルール化はしない (発動点が Claude Code のバイナリ固有で汎用性が無い)

2. **hook のペイロードは event ごとにスキーマが違う。** `background_tasks` は Stop /
   SubagentStop にしか無く、Notification には無い。「Stop で取れたから Notification でも
   取れるだろう」と書いていたら無音で判定不能になっていた (jq が空を返すだけで、
   フックは exit 0 する)。**バイナリの zod スキーマを読んで先に確定させた**のが効いた。
   → 切り出し先: 却下 (既存ルール
   [`measure-external-cli-streams-separately.md`](../../_claude/rules/measure-external-cli-streams-separately.md)
   の「外部ソースは仮説、手元の実験で確定させる」の範囲。今回はスキーマ定義そのものを
   読んだので仮説以上だが、新しい規範は生まれていない)

3. **要望の解釈を 1 回聞いたのが正しかった。** 「bg 中は鳴らすな」は、承認待ちを黙らせると
   誰も気づけなくなる方向 (hook のコメントが「input の見逃しは人が来るまで誰も直せない」と
   明記している) なので、確認せず実装していたら方針を外していた。
   → 切り出し先: 却下 (CLAUDE.md「依頼されたスコープをそのまま完遂する」が既に規定)

## 残課題 — 2026-09-09 にすべて決着

### `make test` の 3 件（起票時は「本セッションと無関係」として残していた）

| 件 | 2026-09-09 の実測 | 決着 |
|---|---|---|
| `test-shellcheck` が `_av1ify_encode.zsh:695` の `&!` で SC1035/1072/1073 | **rc=0 / 指摘 0 件** | ✅ av1ify 側の作業で解消済み |
| `test_av1ify_clipboard.sh` の「Splits on a non-breaking space」 | 再現した | ✅ **原因を特定して直した**（下記） |
| `test_dangling_symlinks.sh` の dangling 4 件 | 再現した（`~/.config/nvim/init.vim*.pre-dein-vim`） | ⏳ **環境側**。`./setup.sh` の再実行で消える。repo の変更では直らないので、ここでは閉じない |

### 🚨 clipboard の失敗は「av1ify 側の担当」ではなく**テストのロケール非固定**だった

production の `__av1ify_absolute_path_words` は
**`[[:space:]]` が UTF-8 ロケールで NBSP (U+00A0) に一致する**性質に依存している
（コードのコメントが「C ロケールは想定しない」と明記済み）。実測:

```
LANG=ja_JP.UTF-8 → a|b   (NBSP で分割される)
LANG=en_US.UTF-8 → a|b
LANG=C           → a b   (分割されない)
```

テストがロケールを固定していなかったため、**`LANG` 未設定の環境でだけこの 1 件が落ちる**
（= 手元だけ赤 / CI は緑）。`tests/lib/utf8_locale.sh` を source する形にした。
そのヘッダが言う「isolate_env を source しないテスト」の **3 例目**。
変異（source を外す）で red を確認済み。

### 人の動作確認 → [issue 343](../343-human-verify-bell-suppression-with-bg-tasks.md) へ切り出した

期限 2026-09-16。「bg あり → 鳴らない / bg なし → 鳴る」の両方向を見てもらう形にした
（**鳴るべきで鳴らない方が問題**なので、そちらを明示した）。

## 切り出しの結論（気づき 1〜3）

1. **対応済み**（hook のコメントに引き方つきで残した。ルール化はしない）
2. **却下**（既存ルールの範囲）
3. **却下**（CLAUDE.md が既に規定）

残課題が空になったので `done/` へ送る。
