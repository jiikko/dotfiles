# 514 (feat): 敵対的レビューを codex に回すかを設定で選べるようにする

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの質問と依頼: 「レビューって opus でやっているの? codex を使った敵対レビューも設定で選択できるようにして」。

## 今どうなっているか (2026-09-26 に transcript で確かめた)

- PM・PG・取り込みの係は `--model` を付けずに起動している (`dispatcher/launcher.go`。`~/.claude/settings.json` に `model` は無い) ので、アカウントの既定のモデル。
  取り込みの係 (df217d0e) の応答 616 件、PG (C-06x) の 1549 件はすべて `claude-opus-5-5`
- 敵対的レビューは PG が自分のサブエージェントで回している (C-06x の PG: opus 8 回・sonnet 10 回。`subagent-model-tiering.md` の選び方)。
  取り込みの係は diff とテストを自分で見る (`integrator-guide.md` の役目 1・2) が、別の視点のレビューは回していない
- codex はどこからも使っていない。ユーザーの auto memory「codex は自発的に起動しない。明示の指示があれば使う」があり、設定で選ぶことを明示の指示として扱う
- codex の実体: `/opt/homebrew/bin/codex` と nodenv の `codex` が PATH にある。対話シェルの `codex` は zsh の関数 (`zshlib/_codex.zsh` が `--approve-for-me` を足す) で、PG の bash からは関数は見えない

## 期待する動作

- 設定 (`~/.config/pro-con/config.toml` と設定画面の「設定」のタブ) で、敵対的レビューの担い手を選べる: `claude` (今のまま。既定) / `codex`
- `codex` を選ぶと、判断ロジック・境界・状態遷移が動いたカードの敵対的レビューを codex に回す (`codex exec`。読み取りのみ)。指摘は今と同じく実コードで裏を取ってから採る
- codex が使えない (実体が無い・認証切れ・利用枠が尽きた: `ratelimit -source codex -check`) ときは、黙って飛ばさず Claude のサブエージェントで代わりに回し、そのことをカードの履歴に残す

## 対応方針 (実装で決めてよい点は PG が決める)

- 誰が codex を呼ぶか (PG が自分の最終ゲートで呼ぶ / 取り込みの係が取り込む前に呼ぶ) は、今の敵対的レビューの置き場 (PG) に合わせるのを第一候補にする
- 役への伝え方: PG への指示 (`dispatcher/prompt.go`) と、取り込みの係・PM の指示書に「設定が codex なら codex でレビューする」を 1 か所から出す (設定の値を指示に差し込む)
- codex は PATH の素の名前でなく実体の絶対パスで呼ぶ (464 の claude と同じ形。`path-shim-must-resolve-real-binary.md`)
- 手順の正本は `~/.claude/skills/codex-review/SKILL.md` の「敵対的レビューの作法」(書き写さない)

## 受け入れ条件

- [ ] 設定の値 (`claude` / `codex`) を config.toml と設定画面で変えられ、`pro-con config show` に出る
- [ ] `codex` のとき、PG (か取り込みの係) の敵対的レビューが `codex exec` で走り、カードの履歴か出力にその証拠が残る
- [ ] codex が使えないときは Claude で代わりに回し、そのことを履歴に残す (偽の codex で失敗させて確かめる)
- [ ] 既定は `claude` で、今の動きが変わらない

## 関連ファイル

- `src/pro-con/config/config.go` / `src/pro-con/ui/settings.go` / `src/pro-con/dispatcher/prompt.go` / `src/pro-con/dispatcher/launcher.go`
- `src/pro-con/integrator-guide.md` / `src/pro-con/pm-guide.md` / `~/.claude/skills/codex-review/SKILL.md`

## 進捗

(まだ無い)
