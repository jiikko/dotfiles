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

### 2026-09-26 C-072 (PG) で実装

- **設定 `review`** (`store.SettingReview`。値は `store.ReviewModes` = claude / codex): 優先は **設定 (`pro-con config set review` / 設定画面) > config.toml の `review` > 既定 claude**
  (`dispatcher/review.go` の `review()`。limit の「設定 > --limit > 既定」と同じ形)。config.toml の書き間違いは起動時に誤り、`config set` の書き間違いは箱に置く前に弾く
- dispatcher は様子 (`dispatcher-state.json`) に担い手・出どころ・codex の実体を書き、`pro-con config show` と設定画面の「設定」のタブ (← → で巡る) がそれを出す
- **誰が codex を呼ぶか: PG** (今の敵対的レビューの置き場に合わせた)。dispatcher が PG の起動の指示 (`Prompt(c, rv)`) に `rv.pgRule` を差し込む: codex の実体の絶対パス・作法の正本 (codex-review の SKILL.md。書き写さない)・
  `ratelimit -source codex -check` が rc=1 / codex が rc≠0 なら Claude で代わりに回して `card attach` で履歴に残す・codex の出力も `card attach` で証拠にする
- 取り込みの係には、知らせ (`integratorNotice`) に **codex の設定で起動した PG のカード** を挙げて「codex の証拠か代わりに回した添付があるかを確かめ、無ければ差し戻す」を足す (`codexReviewLine`)。
  起動のときの担い手はカードの `ReviewBy` に残す (`mark`)。今の設定で決めると、claude で起動した PG を後で codex に変えた設定で差し戻す (敵対的レビューで指摘)
- **PM の指示書には足さない**: PM はレビューに触らない (`pm-guide.md` の役目 5) ので、担い手を知らせても使い道が無い
- codex の実体は、担い手が初めて codex になった Tick に 1 回だけ `ResolveCodex` で解く (claude のままなら解かない = 既定の起動を遅くしない。`ResolveClaude` を `resolveTool` に一般化。shim の辿り方は 464 と同じ)。
  解けなくても dispatcher は止めず、PG には「見つけられなかった (理由)。Claude で代わりに回して残す」を渡す。🚨 1 度解いたら dispatcher を起こし直すまで解き直さない
- 手で書き換えた settings.json の不正な review (`Codex` 等) は使わず、config.toml か既定で動いて理由を様子に出す
- 既定 claude の PG の指示が 514 の前と同じことは、514 の前の版の出力 (`dispatcher/testdata/prompt-before-514.txt`) と比べて固定した
- 🚨 **設定を変えても、起動済みの PG の指示は変わらない** (次に起動する PG から)。取り込みの係は知らせのたびに今の値を受ける
- 設定画面の値の欄を固定幅にしたので、limit / pm の行の説明が 5 桁右へずれた (値の長さが違う行と揃えるため)。画面から review を「設定なし」に戻す口は無い (`pro-con config unset review`)
- 未検証: 本物の PG が codex の設定で `codex exec` を回し添付を残すところ (実機の dispatcher を入れ替えて 1 枚回す必要がある)。偽の codex (`--version` が失敗) を解けたことにしないのは `TestResolveCodexRefusesBrokenBinary`
