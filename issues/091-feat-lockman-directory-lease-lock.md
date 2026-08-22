# 091 feat: ディレクトリ単位の排他を取る CLI `lockman` (SMB 越しの複数マシン想定)

起票日: 2026-08-22
種別: feat
優先度: **P2** (現状は「うっかり二重に走らせない」を人間の注意力で担保している)

## 背景

1 つのディレクトリを複数プロセスから誤って同時に読み書きしてしまうことがある。しかも
そのディレクトリは **SMB 経由で複数マシンから参照する**ため、マシンローカルの排他
(pid ベースの生存判定・`flock`) では届かない。

shell スクリプトの中から

```sh
if ! lockman check "$dir"; then
  echo "他のプロセスが使用中なのでスキップ" >&2
  exit 0
fi
```

のように呼んで、**取るか / スキップするか**を判断したい。lock のメタデータは対象
ディレクトリ自身に置けば、SMB 越しに全マシンから同じものが見える。

## 不変条件 (これを壊す実装は不可)

1. **排他区間は同時に 1 つ** — `acquire` が成功した holder が 2 つ同時に存在しない
2. **持ち主だけが解放できる** — 他プロセス／他マシンの lock を消さない (トークン照合)
3. **holder が死んでもいずれ解放される** — クラッシュ・電源断・ネットワーク断で永久
   wedge しない (TTL + 更新)
4. **判定不能を「空いている」に倒さない** — メタが壊れている・読めない・時刻が取れない
   ときは busy 側 (fail-closed) に倒し、理由を stderr に出す
5. **副作用は対象ディレクトリのメタだけ** — 中身のファイルには触らない

## SMB という前提が課す制約 (設計の中心)

**厳密な相互排除は約束できない。lockman は advisory lease として設計する。**
これを CLI の `--help` と README に明記し、過度な期待を持たせないこと。

- `flock(2)` / `fcntl` byte-range lock は SMB マウントでは実装依存 (macOS smbfs と
  Linux cifs で挙動が違う。マウントオプションで無効化されることもある) → **使わない**
- 排他の原語には `O_CREAT|O_EXCL` でのファイル作成、または `mkdir` を使う (SMB2 の
  create disposition にマップされ、実用上は原子的)。ただし再送・クライアントキャッシュ
  由来の「成功したのに失敗が返る」ケースが理論上ある → **取得後に読み直して自分の
  トークンが入っていることを確認する** (write-then-verify)
- 上書き `rename` の原子性は期待しない
- **時計ずれ**: TTL 判定を各マシンのローカル時計で行うと、ずれた分だけ「まだ生きている
  lock を stale と誤判定」する。対策は共有ストレージ側の時刻を基準にすること
  (メタディレクトリに一時ファイルを作って mtime を読み、サーバ時刻を得る)。
  この補正が入っているかを必ずテストで固定する
- **pid による生存判定は使えない** (別マシンの pid は意味がない)。生存は「TTL 内に
  更新されているか」だけで判断する

## CLI インターフェース案

```
lockman acquire <dir> [--ttl 10m] [--wait 30s] [--label "backup"] [--json]
lockman release <dir> [--token <t>]
lockman renew   <dir> [--token <t>] [--ttl 10m]
lockman check   <dir> [--json]          # 取得せずに空きかどうかだけ見る
lockman status  <dir> [--json]          # 誰が・いつから・いつ切れるか
lockman with    <dir> [--ttl] -- <cmd> [args...]   # 取得 → 実行 → 確実に解放
```

**exit code が実質的な API** (shell から `if` / `case` で分岐するため):

| code | 意味 |
|---|---|
| 0 | 成功 (acquire: 取得できた / check: 空いている) |
| 3 | **他者が保持中** (これは「異常」ではない。スキップの合図) |
| 4 | 保持者が自分ではない (release/renew の対象違い) |
| 1 | エラー (I/O・権限・メタ破損・判定不能) |

- `acquire` は成功時に **トークンを stdout に 1 行**出す。`--json` で機械可読
- 人間向けメッセージは stderr。stdout は機械が読む面に保つ
- **`with` を主用途として推し、`acquire` + 手書き `trap` は逃げ道に留める**
  (shell の `trap` は `set -e` / サブシェル / kill -9 で簡単に漏れる。解放漏れは
  「TTL 切れまで全マシンが止まる」形で効くので、構造で防ぐ)

## メタデータ

- 置き場所: `<dir>/.lockman/` (既定)。`--meta-dir` で対象外へ逃がせるようにする
  (対象ディレクトリを rsync / バックアップする用途だと lock ファイルが混ざるため。
  除外パターンを README に書く)
- 内容 (JSON): `token` (UUID) / `host` / `user` / `pid` (診断用。判定には使わない) /
  `acquired_at` / `expires_at` / `ttl` / `label` / `lockman_version`
- 判定に使うのは **token と expires_at だけ**。他は人間が原因を追うための情報

## 実装場所 — Go で `src/lockman` + `bin/lockman` ラッパを推す

理由:

- **複数 OS で同じ挙動が要る** (macOS と Linux の両方から同じ SMB 共有を触る)。
  shell だと `stat` / `date` / `mktemp` の BSD・GNU 差を全部踏む
  (このリポジトリで実際に何度も踏んでいる)
- `O_CREAT|O_EXCL` / `fsync` / 書き込み後の読み直し検証を素直に書けるのは Go
- **shell で書くと race を作り込む**。既存の mkdir ベース lock は
  [078](078-refactor-resurrect-lock-owner-two-impls.md) のとおり owner 判定が 2 実装に
  分裂しており、その drift が [068](done/068-bug-snapshot-health-lock-owner-format-drift.md)
  の実バグになった。同じ轍を踏まない
- `src/` に新規プロジェクトの規約 (Makefile の `lint`/`test` + root の `GO_PROJECT_DIRS`
  登録 + `.github/workflows/src_lockman.yml` の 3 点セット) が既にある → [src/README.md](../src/README.md)

既存の tmux resurrect 系 lock との関係: **初版では統合しない**。あちらは
「単一マシン・pid 生存ベース」で、lockman は「複数マシン・TTL ベース」と前提が違う。
078 の重複解消は別軸として進め、lockman が実運用に耐えてから統合を検討する。

## 検証計画

ローカルで必ずやること:

- N プロセス (16 並列程度) が同時に `acquire` → **成功したのはちょうど 1 つ**を固定
- holder を `kill -9` → TTL 経過後に他プロセスが取れる / TTL 前は取れない
- **時計ずれ**: 判定側の時計を前後にずらして、誤って stale 判定しないこと
- メタが壊れている / 空 / 権限なし / ディスクフル → busy 側に倒れ、理由が stderr に出る
- `release` に他者のトークンを渡す → 消さずに exit 4
- `with` の子プロセスが異常終了・シグナル死しても解放される

**実 SMB 越しの複数マシン検証は人間しかできない** → 実装後に `human` issue を切って
「2 台から同時に叩いて片方だけが取れること」を確認してもらう。ローカル検証だけで
「SMB でも安全」と書かないこと。

## 未決 (着手前にユーザー判断が要る)

1. **shared (read) lock を持つか**。「参照中のマーク」を複数プロセスが同時に付けたい
   のなら shared/exclusive の 2 種類が要る。初版は排他のみで足りるか
2. メタの既定の置き場所は対象ディレクトリ直下 (`.lockman/`) でよいか
3. `--wait` の既定 (待たずに即 exit 3 / 既定で少し待つ)
4. 1 ディレクトリ 1 lock でよいか、サブキー (`lockman acquire <dir> --key import`) が要るか
