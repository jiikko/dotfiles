# 669 (feat): zundamon-kaisetsu にコース (複数レッスンの掛け合い動画) を作るモードを足す

起票日: 2026-10-08

> **保留の理由と再開の trigger**: 今の 1 本の解説動画では収まらない**巨大なトピック**を扱うことになったら着手する。
> それまでは作らない (ユーザー判断 2026-10-08:「今度使うかもしれないレベル」)。

## 概要

[YES (Yuya's Explainer Skills)](https://github.com/yuya-takeyama/agent-plugins/tree/main/plugins/yes) の `course` を参考に、
大きなトピックを 3〜5 分のレッスン数本に分けた掛け合い動画を、目次・進捗つきの 1 つの HTML にまとめるモードを
zundamon-kaisetsu に足す。skill の最初に「1 本の解説 / コース」を選べるようにする。

## 決めたこと (2026-10-08 の検討)

- **YES の plugin を入れて併用せず、仕様だけを移植する**。併用すると次が 2 つずつになる:
  VOICEVOX の起動と停止 (YES は Docker 専用の `bin/yes-speak` / こちらは container 優先の自動起動と見張り)、
  立ち絵の入手 (YES は `characters.py fetch` で `~/.cache/video/characters/` へ取得 / こちらは `assets` サブモジュール)、
  プレイヤーと build (YES の板書は自由な HTML の `scenes.html`、こちらは型の決まった `show`。YES の標準の出力は HTML だけで、
  mp4 は例の補助スクリプト `docs/examples/yes-intro/record.py` だけ。読みの見直しは YES が `readings.json` + lint の警告 + wav を聴く、
  こちらは `kana` の書き出しとサブエージェントの点検。YES の course に載せるとこちらの mp4・`show`・読みの工程が使えない)
- YES の LICENSE は MIT (Copyright (c) 2026 Yuya Takeyama)。**コードを移植するなら MIT の著作権表示と許諾文を移植先に残す**
  (README に出典を書くだけでは足りない)。仕様・振る舞いだけを参考に書き直すなら出典の記載でよい。
  YES の同梱デモの声・立ち絵は MIT の対象外 (`plugins/yes/THIRD_PARTY_NOTICES.md`)
- **確認テスト (YES の `quiz`) は入れない**。ユーザー判断 2026-10-08 で不要とした。YES の course は単位として video / slides / quiz を
  混ぜられるが、こちらのコースは掛け合い動画だけを並べる (間違えた問題の復習など、quiz に依る画面も持ち込まない)
- **slides / explain も入れない**。キャラも声も使わない形式で、この skill の枠の外。要るときは YES をそのまま使う

## 対応方針 (着手時の叩き台。着手時に YES の最新版と照らし直す)

参照元: YES の `skills/course/references/authoring.md` (2026-10-08 時点)。

1. **`course.json`** にレッスンを並べる (`title` / `summary` / 各レッスンの台本 `script.json` へのパス)。build がレッスンごとに
   今の synth / build を回し、1 つの HTML にまとめる。台本の形式は変えない
2. **分け方** (SKILL.md の手順に足す): 資料から 3〜5 分のレッスンを 4〜8 本。各レッスンは別々の部分を受け持ち、2 つの資料が
   両方触れている事柄はどのレッスンが扱うかを先に決める。他のレッスンの予告だけをする「概要」のレッスンは作らない
   (導入のレッスンは、他のどれも教えないことを教えるときだけ)
3. **台本を書いた後の編集の点検**: レッスン間の重複 (同じ事柄を 2 本で教えていないか)、コースの構成への言及
   (「次のレッスンで」「この講座では」) が無いかを見る。今の校正・裏取りの工程はレッスンごとにそのまま回す
4. **画面** (YES の振る舞いのうち、テストを除いたもの): ホーム (進捗・「続きから」・レッスンの一覧)、目次 (狭い画面ではドロワー)、
   何もロックしない。実際に再生した区間が 85% に達したら視聴済み (シークで飛ばした分は数えない)。
   進捗は localStorage に置き、使えない環境 (サンドボックスの viewer) ではメモリに置いて動き続ける。
   ディープリンクは YES では `#home` / `#<レッスン>` (そのレッスンの最初の未完了の単位を開く) / `#<単位 id>/t=12.3`
   (単位 id の既定は `<レッスン>-<種類>`)。こちらは単位が動画だけなので、形は着手時に決める
5. **大きさ**: 1 本 10 分で約 5MB (音声 64kbps。SKILL.md の表) なので、4〜8 本 × 3〜5 分では 16MiB (Claude の Artifact の上限) を
   超えうる。コースの既定のビットレートを下げる (YES は course が 32k、単体の video が 48k) か、`--max-bytes` で上限を検査する
6. **mp4**: コースは HTML だけ。mp4 が要るならレッスンごとに今の `--format mp4` で書き出す
7. コースの外枠は `templates/player.html` (270 行) に足さず、別のテンプレートに分ける

## 着手時に確かめること

- YES の course の現行の仕様 (この issue の記述は 2026-10-08 時点の要約)
- 1 つの HTML に複数レッスンのプレイヤーを載せるとき、今の `player.html` の状態 (再生位置・`#t=` のハッシュ・`requestAnimationFrame` の描画)
  がページに 1 つしか無い前提で書かれていないか

## 関連ファイル

- `_claude/skills/zundamon-kaisetsu/SKILL.md` / `templates/player.html`
- `src/zundamon-kaisetsu/` (`build.go` / `script.go` / `main.go`)

## 進捗

- 2026-10-08: 起票のみ。未着手
- 2026-10-08: 反証レビュー (sonnet 1 体。YES の README・LICENSE・course / video / zundamon-video の SKILL.md と authoring.md・
  build.py・shell.js を実読) を通した。直したもの: 「YES のプレイヤーには mp4・図解・読みの見直しが無い」は誤り
  (板書の仕組み `scenes.html` と読みの辞書はある。mp4 は標準の出力に無い、が正しい) / YES の course は quiz・slides を混ぜられる事実 /
  ディープリンクの形 / MIT の表示義務 / 概要レッスンの例外条件 / video の既定 48k。
  反証できなかったもの: VOICEVOX は Docker 専用、立ち絵のキャッシュ取得、85%・シーク不算入、localStorage の代替、4〜8 本 3〜5 分、
  course の既定 32k、64kbps で 10 分約 5MB、`player.html` 270 行、MIT
