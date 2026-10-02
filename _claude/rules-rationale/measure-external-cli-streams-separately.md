# 外部 CLI の stdout / stderr / exit code は分離して実測する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/measure-external-cli-streams-separately.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (起源: retro 100 項目 1・4, 2026-08-24)

外部 CLI の出力を `2>&1 | head` でまとめて測ったため「判定材料は stdout にある」と誤認し、それを
設計ファイルに**実測事実として**書いた。実装者はその記述どおりに stdout を読む実装を書いた。
分離して測り直すと判定材料は stderr で、stdout は 0 バイトだった —— 気づかず進んでいたら
**判定が常に unknown に落ちて一度も発火しない機能**が完成していた。同じ測り直しで、別 CLI の
exit code も初回の記述 (「未ログインでも exit 0」) が誤りだと分かった (初回は exit を測っていなかった)。

効いたのは stream の分離そのものより、**誤った実測表を検出する経路を別に持っていたこと**だった。
seam を全部差し替えたテストは「実測表どおりに書いたコードが実測表どおりに動く」ことしか示さず、
実測表が誤っていた今回は何も守らなかった。副作用を隔離した環境で実 CLI を 1 回通して初めて
「本当に動く」と言えた。

🚨 この節に個別 CLI の値を書かないのは意図的 (上のルール 4 番目)。当時の実測仕様は
`src/glogx/cli_health.go` のコメントが正本。


## `--help` は契約ではない (2026-09-04)

実例と実測は `rules-rationale/verify-execution-not-just-exit-code.md` の
「確認を求める外部コマンドは…」節に置いた (同じ日・同じ CLI・同じ作業で出た 2 つの誤りなので、
片方に寄せる)。要点だけ: `docker builder prune --help` の `-a` の説明
("Include internal/frontend images") と、実行時の警告文が示す実際の範囲
(`-a` 無し = dangling のみ / `-a` 有り = 全部) が食い違っていた。

## 本文から移した実例 (2026-09-28 の prompt-audit。本文は規範だけにするため)

- 実測日: 2026-09-08 / 出典: swift-smbee issue 092 / done/090
- 実例 obaket 645 M6c, 2026-09-03: Swift Testing の `started` 行 (stdout) と NSLog マーカー (stderr) の順序から hang の位置を 推理して 2 回誤診。stderr だけに揃えた 5 run 目で確定
- 実測 2026-09-25 dotfiles 427: fake の再開は同じ session を続ける前提で、 本物の `claude --bg --resume` は別の id を立てていた。その前提の上で敵対レビューを 5 周重ねた

## ログ 1 行の解釈・周期的な外部呼び出し (起源: dotfiles issue 627 / retro 631, 2026-10-02)

- ログ: Claude Code の debug ログの「fetchUtilization: 429 remembered for this bearer; not asking again for 2692s」を、
  「CLI が覚えている間はサーバへ行かない」と読み、429 の後に止める時間を 10 分にした。CLI のコード (`fetchUtilization`) を
  読むと、覚えは `claude -p` のプロセスをまたがず、起こすたびに実際に GET して新しい 429 を受け取っていた
  (秒数が 2692 → 2849 と伸びていたのが証拠)。10 分ごとに 429 を叩き直し、窓を延ばしていた。止める時間は 50 分に直した
- 周期: glogx は利用枠の表示中、60 秒ごとに `claude -p /usage` を起こしていた (2026-07-22 の要望)。周期を決めた時の判断は
  「トークン課金ゼロ」「1 回 2 秒」というローカルのコストで、サーバの `/api/oauth/usage` の rate limit は考えていなかった。
  表示を開いたまま 17 分 (17 回) で 429 になり、以後約 45 分は取れなかった。bin/ratelimit (hook) と pro-con も同じ口を
  それぞれの周期で叩いていたので、呼び出し元ごとに周期を下げても合計は減らない。直し方は全呼び出し元が通る共有ゲート
  (5 分に 1 回まで) と、応答ヘッダ由来で既に届いている statusline の値を主な出所にすること
