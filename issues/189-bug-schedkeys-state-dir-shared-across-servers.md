# 189 bug: 予約の状態ディレクトリが全 tmux サーバで共有で、一覧・取消が別サーバの予約を混ぜる

起票日: 2026-09-02
関連: `scripts/tmux_schedule_keys.sh` (`jobs_tsv` / `pane_label` / `cancel_selected` /
`prune_stale` / `refresh_pane_indicator`) / issue 166 (pane 表示。同じ原因の**表示側だけ**を直した) /
issue 186 (166 の目視確認)

## 症状 (コードで確認 / 実サーバでの再現は未実施)

`STATE_DIR` は socket ごとに分かれていない:

```sh
STATE_DIR="${TMUX_SCHEDULE_KEYS_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/tmux-schedule-keys}"
```

一方 pane id (`%5` 等) は**サーバごとに独立採番**される。したがって複数の tmux サーバ
(本番の default サーバ + `-L` の隔離サーバ / scratch サーバ) が同時に居ると、同じ
ディレクトリの `.job` を両者が読む。

1. **一覧に別サーバの予約が出る**: `jobs_tsv` は `$STATE_DIR/*.job` を socket で絞らずに
   全件列挙する。送り先の表示名は `pane_label` が **今のサーバ**に `display-message -p -t <pane>`
   で問い合わせて作るので、別サーバの `%5` が自サーバの `%5` の名前で出る (自サーバに
   その id が無ければ「(消滅)」と出る)。つまり**別人の予約が、自分の pane の名前で並ぶ**
2. **その行を選ぶと取り消せてしまう**: `cancel_job` は job ファイルを rename して消し、
   `pid_is_sleeper` は `ps` の command line で照合する (サーバ非依存) ので kill も届く。
   別サーバに仕掛けた予約を、別のサーバの UI から取り消せる
3. **失効通知の件数が混ざる**: `prune_stale` の「サーバ再起動などで N 件の予約が失効しました」
   の N は他サーバ由来の job を含みうる

issue 166 で入れた pane 表示については、同じ原因を**表示側だけ** socket + サーバ pid の
照合で塞いだ (`refresh_pane_indicator`)。一覧・取消・失効通知は**未対応**。

## 前提の実測 (2026-09-02)

「複数サーバが同時に居る」は仮定ではなく日常。起票時点の実測:

- 稼働中の tmux サーバプロセス **6 個** (default 1 + `-L` の隔離サーバ 5。`ps -Ao pid,command`)
- `/private/tmp/tmux-501/` の socket ファイル **199 個** (過去の probe の残骸を含む)
- `TMUX_SCHEDULE_KEYS_DIR` を設定しているのは `tests/` と `tmp/` の使い捨てスクリプトだけで、
  `_tmux.conf` / zsh 側は**設定していない** (`grep -rn TMUX_SCHEDULE_KEYS_DIR`)。
  つまり実運用の全サーバが既定の `~/.local/state/tmux-schedule-keys` を共有する

## 何が正しいか (不変条件)

- **ある tmux サーバの UI に出る予約は、そのサーバの予約だけ**。他サーバの予約は見えず、
  取り消せない
- 送り先の表示名は、その予約を作ったサーバに問い合わせて作る (別サーバに聞いた名前を出さない)

## 直し方の候補

| 案 | 内容 | 利点 | 難点 |
|---|---|---|---|
| A. `STATE_DIR` を socket ごとに分ける | `$STATE_DIR/<socket を安全化した名前>/` | 絞り込みのコードが要らない (構造で保証)。`prune_stale` の孤児掃除もサーバ単位で閉じる | socket path の正規化が要る。既存の `.job` の移行 (無視して失効させるのが素直) |
| B. 読む側で socket + サーバ pid を照合する | `jobs_tsv` / `cancel_selected` / `prune_stale` に絞り込みを足す | 既存ファイルのまま。166 の `refresh_pane_indicator` と同じ形 | 照合を足す箇所が 3〜4 つに散る (1 箇所忘れると再発する。166 の P1 がまさにその形) |

推し: **A**。「読む側が毎回絞る」形は絞り忘れが 1 箇所でもあれば漏れるので、構造で閉じる方が安い。
ただし `prune_stale` の「サーバが死んだ後に残る job」を誰が掃くかは A では要検討
(サーバごとのディレクトリが増え続けるので、空になったディレクトリを掃く経路が必要)。

## テスト観点

- `tests/tmux/test_schedule_keys.sh` の stub 方式で「別 socket / 別サーバ pid の job は
  一覧に出ない・取り消せない・失効件数に数えない」を固定する。166 で足した
  「別サーバの予約は同じ pane id でも数えない」と同じ形が使える
- 1 経路ずつ変異 (絞り込みの削除) で red を見る

## 直す順番

**2 (取消) を最優先**。3 つの中でこれだけが「別サーバの本物のプロセスを外から kill する」
実害を持つ (一覧と失効件数は誤表示で止まる)。

## レビュー状態

反証レビュー済み (read-only サブエージェント 1 体、codex は未使用)。**反証できた主張は無く、
3 件とも隔離環境の実験で再現された**:

1. **一覧**: `STATE_DIR` に別サーバの job (別 socket / 別 srvpid / pane `%7`) を置き、
   自サーバとして `jobs_tsv` を呼ぶと、その予約が**自サーバの pane 名で**一覧に出た
2. **取消**: 別サーバの予約に対して本物の `fire` を子プロセスとして起こし、自サーバとして
   `cancel_selected` を呼んだところ、job が消え、**別サーバの sleeper プロセスが kill された**
3. **失効件数**: 自サーバの stale 1 件 + 別サーバの stale 1 件で `prune_stale` を呼ぶと
   「2 件の予約が失効しました」と合算して通知された

行番号つきの確認も一致 (`jobs_tsv` / `pane_label` / `cancel_selected` / `cancel_job` /
`prune_stale` に socket の照合は無く、166 で入れた照合は `refresh_pane_indicator` の
表示専用でこの 3 経路からは呼ばれない)。候補 A の「孤立ディレクトリを掃く経路が無い」も
grep で裏が取れた (`scripts/tmux_reap_orphan_servers.sh` はプロセスしか掃かない)。

レビュワーの実験手順 (関数だけを source して個別に叩く / 本物の `fire` を起こして kill を
確認する) は、上の「テスト観点」の固定テストにそのまま転用できる形。

## 適用ログ (2026-09-03)

**案 A を実装**した (commit `dad6538` + 後続)。

- `resolve_state_dir` が socket から `$STATE_ROOT/<socket を % で潰した名前>` を決める。
  解決するのは wizard だけで、socket が取れなければ**予約を作らない** (fail-closed。
  共有 dir へ落とすと混ざりが戻る)
- `fire` は `run-shell` のコマンド文字列で `TMUX_SCHEDULE_KEYS_DIR` を受け取る。
  `run-shell` の子は `$TMUX` を持たないことがあるので自分では解決できない。
  **env 前置きが子に届くことは隔離サーバで実測** (`got=/tmp/per-socket-dir`)。
  置き場を渡されなかった `fire` は黙って exit 0 する (存在しない予約の破棄を通知しない)
- **旧共有 dir の job は移さない**。移すと眠っている sleeper の claim (job の rename) が
  失敗し、予約が黙って発火しなくなる。古い予約はそのまま発火し、一覧には出なくなる
  (適用時点で予約は 0 件だったので、実際にはこの穴を踏んでいない)

### 案 B を採らなかった理由

`jobs_tsv` / `cancel_selected` / `prune_stale` の 3 箇所に絞り込みを足す形になり、
issue 166 でその 3 箇所を落とした実例がある。さらに B では `prune_stale` に
「記録されたサーバ pid がもう生きていない job は socket が違っても掃く」という
第 3 の分類が必要で、掃除の設計判断は B でも避けられない。

なお、この repo の既存パターン (`tt_on_default_server` による socket ゲート) は B 寄りだが、
あちらは**共有リソースにテストサーバを書かせない**ゲートで、持ち主は 1 つ。予約は
各サーバが自分の分を所有する状態なので、入れ物で分ける方が構造に合う。

### 破壊性

- セッション・pane には触らない。`_tmux.conf` を変えないので reload もサーバ再起動も不要
- 眠っている sleeper は無傷。末尾に dispatch があるスクリプトは全体が parse 済みで、
  `exit 0` で抜けるため走行中の書き換えを読み直さない (実測。関数本体は旧版のまま)
- 旧共有 dir は消さない (残骸として残る。中身は数バイトの job)

### 検証

- 実 tmux (隔離 `-L` サーバ) で end-to-end: socket ごとの dir に置いた job が発火して
  pane に文字列が届き、job が掃かれ、pane 表示も消えた
- 変異検証 4 本。**初回は 1 本 (fire の置き場ガードの削除) が緑のまま生き残った**ので
  「置き場を渡されなかった fire は何も送らず何も通知しない」を固定して red にした。
  他の 3 本 (共有 dir へ戻す / fail-closed を外す / env 前置きを外す) は初回から red

### 残り

- 旧共有 dir に残った job があれば、一覧に出ないまま発火する。0 件のときに適用したので
  現状は空。気になるなら `~/.local/state/tmux-schedule-keys/*.job` を目視で確認する

## 敵対的レビュー (2026-09-03、opus。codex は未使用)

案 A の実装に対して 8 件。**対応 6 件 / 残し 1 件 / 情報 1 件**。

### 直したもの

- **P2-1 `run-shell` のコマンド文字列は tmux がフォーマット展開する** (再現つき)。socket path に
  `#{pane_id}` が入ると wizard の置き場と fire の置き場がズレ、「破棄」と「予約に失敗」が
  同時に出て予約が成立しない。`#` を `##` に潰す `tmux_escape` と、`'` を正しく閉じ直す
  `sh_quote` を通してから埋めるようにした (実 tmux で往復一致を確認)
- **P2-2 「別サーバの dir は見えない」テストが何も守っていない** (実験で証明された)。fixture を
  「退行したら見えるようになる場所」= 旧共有 dir にも置く形へ直した
- **P2-3 同じ socket にサーバが立ち直ると症状が dir 内で復活する** (再現つき)。前任サーバの
  job (記録された pid が今のサーバと違う) を `prune_stale` で失効させる。kill はしない
  (job を消せば前任の sleeper は claim に失敗して静かに降りる)
- **P3-1 `/` → `%` の潰し方で別 socket が衝突する**。`%` を先に `%25` へ逃がす
- **P3-2 env 指定の置き場が検査を通らないのに、コメントが「保証している」と書いていた**。
  env 経路にも同じ検査をかけ、コメントを実装に合わせた
- **P3-3 socket path に `'` があると機能が丸ごと使えない**。`sh_quote` で扱えるようになったので
  弾くのをやめた (弾くのは改行だけ)
- **P3-4 テストが `XDG_STATE_HOME` を隔離しておらず false red + 外部へ job を書き残す**。
  テスト冒頭で `unset` した
- **P3-5 サーバ環境に `TMUX_SCHEDULE_KEYS_DIR` が居ると黙って共有 dir へ戻る**。
  env 指定が効いているときはログに残す (無音で無効化されない)

### 残すもの

- **P2-4 二度と使われない socket の dir に残骸が残り、通知もされない**。旧設計ではどのサーバの
  wizard でも全 job を prune できたが、今は自 dir しか見ないため、`-L` の使い捨てサーバが
  残した `.job` / `.pid` は永久に残る (dir も socket ごとに増える)。
  **対応しない**: 残骸は数バイトのテキストで、使い捨てサーバは popup と UI を経ないので予約を
  作ることがほぼ無い (実測時点で `~/.local/state/tmux-schedule-keys/` は空)。
  掃除機構は破壊的操作の新設なので、実際に溜まった証拠が出てから作る。
  **再開の trigger**: `find ~/.local/state/tmux-schedule-keys -name '*.job' | wc -l` が
  予約していないのに 0 でないとき

### 変異検証 (2 周目)

修正に対して 5 本。**初回は 2 本が緑のまま生き残った**ので直した:

- 前任サーバの prune を外す変異 → 前任 job に偽 pid を使っていたため、通常の stale 掃除が
  先に消していた (P2-2 と同じ形)。本物の sleeper を起こす fixture に直して red
- env の置き場の改行検査を外す変異 → 検査そのものが未テストだった。「改行を含む置き場では
  sleeper を起こさない」を足して red

他 3 本 (`%` の逃がしを外す / `tmux_escape` を外す / サーバ pid の有無ガードを外す) は初回から red。
