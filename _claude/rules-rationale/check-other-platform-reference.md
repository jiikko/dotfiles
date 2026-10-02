# Platform 特有のバグ調査では、必ず他 platform に動いている参照実装がないか確認する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/check-other-platform-reference.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: DualNote iOS #030 IME バグ, 2026-05-23)

iOS の UIViewRepresentable wrapper が壊れていたとき、iOS だけ見て delegate cycle 仮説に固執し 3 回試行して全て外した。forge の専門家は macOS の**同機能の動いている wrapper** と構造比較を一発で実施し、真因 (双方向 Binding と `becomeFirstResponder()` 同期呼出という構造的差分) を 5 分で特定した。

## 「別の入口」への拡張 (起源: dotfiles issue 627 / retro 631, 2026-10-02)

glogx が `claude -p /usage` で取る Claude の利用枠が、サーバの 429 で取れなくなった。同じ時刻に対話セッションの `/usage` は
普通に見えていたのに、壊れている `-p` の側だけを見て「`/usage` = `/api/oauth/usage` を叩く」と決め、共有ゲート (間引き・停止)
を作り込み、敵対的レビューを 3 周回した。ユーザーの「このセッションの /usage は見えている」で CLI のコードを読むと、対話側は
推論の応答ヘッダで受け取った値で答えていて、サーバに聞いていなかった。出所そのものを statusline の値 (応答ヘッダ由来) へ
変えることになり、ゲートは予備の経路に格下げになった。platform の違いではなく「同じ道具の別の入口」だったが、
動いている側と比べれば最初の 5 分で分かった形は同じ。
