# 618 (design): Claude Code の mods (関数 hook の plugin) へ、settings の hook の一部を移す

起票日: 2026-10-02

> epic 618 の親 issue。判断の正本 (どれを移す / 移さない、前提、未確認の点) はここに置き、作業は同じディレクトリの子 issue で進める。

## 概要

Claude Code 2.1.287 に mods (関数 hook の plugin) が入った
(記事: https://claude.dev/blog/getting-started-with-claude-code-mods/ 、API の正本は plugin-authoring skill が書き出す `claude-code.d.ts`)。
settings の hook (`_claude/settings.json` の command) と比べて、次ができる:

- セッションに 1 回読み込まれて常駐し、状態をメモリと `$.state` / `$.store` に持てる (イベントごとに bash を起こさない)
- UI を描ける: プロンプトの上の帯 (`AbovePrompt`) / ペイン (`Pane`) / `$.ui.status` / `$.ui.toast`
- システムプロンプトに節を足せる (`prompt.compose`)。モデルが呼べるツール (`$.tool.register`) とスラッシュコマンド (`$.command.register`) を足せる
- settings の hook のイベントも `classic.<Event>` として受けられる。結果は settings の hook と同じ形 (`block` / `additionalContext` / PreToolUse の `allow`・`ask`・`deny`)。
  型定義の記述では、settings の hook が 1 本も無くても発火する

制約: モジュールは Node も DOM も無い隔離環境で動き、外の世界には `$` (fs / process / http / model …) 経由でしか届かない。
**API は early access で、リリースごとに変わる** (skill の reference.md が明記)。

## 判断: どの hook を移すか

settings に配線されている hook の script 16 本 (エントリは 25) (`_claude/hooks/*.sh`) を、mod にすると**何が新しくできるか**で分けた。
「移せるか」では分けない (`classic.*` があるので、型の上では全部移せる)。

| hook | 判断 | 子 issue |
|---|---|---|
| `issue-rules-inject.sh` (SessionStart で規約を注入、4 本) | **仮説を測ってから決める**。「システムプロンプトの節にすれば、今の `<system-reminder>` より守られる」は未検証の仮説。620 で A-B を取り、守られる率が上がったときだけ移す | 620 |
| `human-tasks-due.sh` / `retro-open.sh` | **移す**。人へ見せる情報なので帯に出す。モデルに伝えさせるのをやめ、文脈を空ける | 621 |
| `tmux-pane-state.sh` (6 イベント。PostToolUse はツール呼び出しのたびに bash を起こす) | **保留 (実測待ち)**。状態を常駐側に持てるが、移す利点 (速度) が未計測。622 で before を測り、閾値を超えたら移す | 622 |
| `warn-discarding-checkout.sh` | **検討**。人に選ばせる形にしたいが、settings の hook の PreToolUse `ask` でも標準の確認で同じことができる。mod が要るかを 623 で先に決める | 623 |
| `ratelimit-warn.sh` / `next-claim-unshared.sh` | **今は移さない**。モデルに行動 (ユーザーへの提案 / push の伺い) をさせるための注入で、帯に出すだけでは役目を果たさない。621 の帯に同じ情報を足すかは 621 で決める | — |
| `deny-bare-tmux-kill.sh` / `deny-piped-push-then-destroy.sh` | **移さない**。守りの hook。mod の hook は失敗すると黙ってスキップされ、chain が続く (reference.md「Developing one」)。settings の hook のままにする | — |
| `issue-progress-check.sh` (Stop で差し戻す) | **今は移さない**。`classic.Stop` の `block` で移せるが、新しくできることが無い | — |
| `gofmt-on-edit.sh` / `git-state-verify.sh` / `next-claim-push.sh` / `normalize-settings.sh` / `claude-links-sync.sh` / `issue-progress-start.sh` | **移さない**。移しても新しくできることが無い。`claude-links-sync` と `normalize-settings` はセッションの外の状態 (link・settings ファイル) を直す仕事 | — |

**移さないと決めたものは、移植のコストを払う理由が出たとき (bash の起動が遅いという実測が出た、等) に見直す。**

## mod が黙って止まったときの扱い (移すものすべてに適用)

mod の hook は失敗するとスキップされ、知らせは debug log か (ホットリロード中だけ) transcript の 1 行に出るだけ
(reference.md「Developing one」)。Claude Code の更新で API が変われば、読み込まれないまま気づかれない。
settings の hook は失敗すれば stderr に出る。**移したものは、壊れ方が「出る」から「黙る」に変わる**。

- 移す子 issue は、それぞれ「mod が読み込まれなかったとき何が起きるか」と、それを検出する手段 (canary・A-B) を受け入れ条件に持つ
- 「黙る」ことが壊れ方そのものになる hook (`issue-rules-inject.sh` は冒頭に「規約が届かないことがこのフックの壊れ方」と書いて黙らない設計にしている) は、
  移した後も、規約側の文 (`_claude/CLAUDE.md` の義務の 1 行) を残す
- 切り替えの commit ごとに、読み込まれる経路 / 読み込まれない経路の両方の期待値を書く (「1 本ずつ切り替える」運用だけで二重・欠落を防がない)
- **切り替えの commit を `~/dotfiles` へ pull した時点で動いているセッション**の期待値も書く。mod のフォルダは保存で読み直されるが、
  settings の env (読むフォルダの一覧) は起動時に読まれる。settings の hook が起動時の snapshot かは未確認。
  env にフォルダを足す commit は、hook を外す切り替えの commit より前に分けて入れる

## 前提と未確認の点 (619 で最初に潰す)

**619 の 3 経路の実測結果は、620〜623 の着手条件にする**。読まれない経路があれば、その経路で要る settings の hook は残し、この表を直してから着手する。


- [ ] **読み込まれる経路**。settings の hook は対話と `claude -p` では走るが、**pro-con の PG・PM・取り込みの係では走らない**
      (`--setting-sources project,local` で起動し、ユーザーの settings.json の hook と env が外れる。`src/pro-con/dispatcher/launcher.go` の
      `persistentSessionArgs`、`rolesettings.go` 冒頭。issue 431)。移す hook は今も PG に効いていないので、PG で mod が読まれないことは退行ではない。
      reference.md によれば `CLAUDE_CODE_PLUGIN_DIRS` は、`~/.claude/settings.json` (= `_claude/settings.json` への link) の `env` に加えて**プロセスの環境**からも読まれる。
      dispatcher は PG を `os.Environ()` (tmux の変数だけ外す) で起こすので、Claude のペインの中から pro-con を起動すると、
      その経路の PG にだけ mod が載りうる (431 の隔離が経路で変わる。未実測)。dispatcher で `CLAUDE_CODE_PLUGIN_DIRS` を外すか、載せると決めるかを 619 で決める
- [ ] **subagent / fork**。`prompt.compose` で足した節が subagent のシステムプロンプトにも入るかは未確認 (今の SessionStart の注入は subagent に届かない)
- [ ] **二重に効かないこと**。移行の途中で、mod と settings の hook が同じ注入・同じ表示をしないようにする (1 本ずつ切り替える)
- [ ] **API の変化への備え**。Claude Code の更新で mod が読み込まれなくなったとき、何が黙って止まるかを列挙しておく。守りの hook を移さないのはこのため

## 子 issue

- [ ] 619 — 土台: 置き場所・読み込みの配線・テスト・入口の文書 (**他の全部の前提**)
- [ ] 620 — issue 規約の注入をシステムプロンプトの節へ (最初の 1 本)
- [ ] 621 — human / retro の催促をプロンプトの上の帯へ
- [ ] 622 — tmux のペインの状態表示を mod へ (実測の結果で移すかを決める)
- [ ] 623 — 未コミットの変更を捨てる checkout の前に、人に選ばせる (mod か settings の `ask` かを先に決める)
- [ ] 625 — Claude desktop (Code タブ) にも CLI と同じステータスバーを出す (desktop が `statusLine` を描くかを先に確かめる)

順番は 619 → 620 → 621 (620 と 621 はどちらも `_claude/issue-rules.md` と `_claude/CLAUDE.md` の文面を直すので直列)。622 / 623 は 619 の後なら独立。

> 起票時は 624 (issue の採番を push まで済ませる) も子にしていたが、反証レビューで「script 1 本で同じことができ、mod にする理由が無い」と指摘され、
> epic から外して `issues/` 直下の 624 にした (mod の失敗モードだけが増える)。

## 進捗

- 2026-10-02: 起票 (記事と `claude-code.d.ts` を読み、hook 16 本を分類した)
- 2026-10-02: 反証レビュー 2 本 (事実 / 設計。read-only のサブエージェント、sonnet)。事実は行数など軽微 3 件を訂正。
  設計は採用 9 件: 622 を保留に、623 を settings の `ask` との比較からに、624 を epic から外す、黙って止まるときの扱いの節を足す、
  620 の判定を script 経由に、619 を 620〜623 の着手条件に、620 → 621 を直列に、621 の失敗モード、619 の CI の扱いを決める。
  反証されなかった判断: 守りの hook・`ratelimit-warn`・`next-claim-unshared`・残り 7 本を移さないこと、619 を土台にすること
- 2026-10-02: 敵対的レビュー 1 本 (read-only のサブエージェント、opus。「書いたとおりに実装したら壊れる手順」を探させた)。7 件すべて採用 (根拠は実物で確認):
  PG の前提の誤り (今も PG で hook は走っていない / 親の環境から plugin が載りうる) → 618・619・623 / 624 の push が古い base で通らない → commit-tree の形へ /
  620 の「気づく手段」が `$.store` の古い印で素通りする → session_id ごとの印へ / 620 の節が描画のたびに変わり cache を外す → session.start で 1 回読む /
  619 の `.claude-plugin/types/` が共有の working tree に生成される → .gitignore / 切り替え時に動いているセッション → 618 / pre-push に止められた番号が手元に残る → 624。
  壊せなかった攻め口: 624 の同時実行 (pre-push の一意性検査が止める)・epic 配下の数え漏れ、622 の `TMUX_PANE` の取り違えと本番 tmux への副作用、621 の古い `$.state`
- 2026-10-02: ユーザーとの対話で、620 の前提「システムプロンプトの節へ上げれば拘束力が上がる」に根拠が無いと確認した
  (文面を移しても拘束力は足されず、置き場所と名目が変わるだけ。強まるか弱まるかは測るまで分からない)。620 を「上がるか測る」issue に直した。
  mods の価値は、文面の転記ではなく機械的な止め方 (deny / block / ask) と状態を持つ検査にある、という整理も同時に確認した
- 2026-10-02: 620 の計測計画に敵対的レビュー (opus)。6 件すべて採用 (根拠は実物で確認): 観測用のコマンドを A-B に流用すると書き込めず 0 対 0 / `~/.claude/CLAUDE.md` の
  「Issue管理」が A だけに拘束力を与え B に Read を促す / B を作る手段が本番の settings しか無い → 隔離した config と `--plugin-dir` / 対照 C と `next/` の在る repo /
  `-p` では `/context` が使えず B の節が足されたかを run ごとに確かめる / 一時 repo の置き場所。
  壊せなかった攻め口: 注入の文字数の上限 (全ファイルが内側)、注入の条件、他の SessionStart hook の交絡、prompt cache
- 2026-10-02: 625 (desktop のステータスバー) を起票 (ユーザーの依頼)
