# 176 bug: `/opt/homebrew` がカタログにハードコードされ、Intel Mac (`/usr/local`) で候補 0 件に化ける

起票日: 2026-09-02
重要度: **P2**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 5) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (1 章 Tier 2 の brew-orphan-state / brew-cleanup-residue)

## 対象

`src/doctor/disk/catalog.go` の `brew-orphan-state` / `brew-cleanup-residue` の Paths (`/opt/homebrew/...` 直書き)

## 何が起きるか

Homebrew の prefix は Apple Silicon が `/opt/homebrew`、Intel が `/usr/local`。カタログは前者を直書きしているので、
Intel Mac では glob が 0 件になる。**brew 自体は実在して台帳の取得も成功する**ため、
「診断できず」ではなく **`ok` / items=0 (候補はありません)** と表示される。

実測 (体 5 の probe。`brew-orphan-state` の Paths を `/opt/homebrew-elsewhere/var/*` に差し替えて再現。実証済み):

- 結果: `ok` / items=0
- 台帳 (`brew info --json=v2 --installed`) は成功しているので、失敗の痕跡がどこにも出ない

Intel Mac だけでなく、`HOMEBREW_PREFIX` を非標準の場所に置いている環境 (linuxbrew 形式の `~/.linuxbrew` 等) でも同じ。

## 再現手順

Intel Mac、または prefix を変えた環境で:

```
brew --prefix          # /usr/local が返る
<diskdoctor> -json | jq '.results[] | select(.id|startswith("brew")) | {id,status,items}'
```

`status=ok, items=null` になる (実際には `/usr/local/var` に孤児があっても検出されない)。

## 対応案

- prefix を `brew --prefix` (または `HOMEBREW_PREFIX` 環境変数) から取り、カタログのパスを組み立てる
- prefix が取れなかったら **fail-closed** (「診断できず」)。取れた prefix が存在しないときも同じ
- 併せて、glob が 0 件でも親ディレクトリ (`<prefix>/var`) が存在しない場合は「診断できず」に倒す
  (issue 175 と同根なので、どちらの対応でも片方は解決する)

## 受け入れ条件

- [ ] prefix が動的に解決される (偽 prefix で probe テスト)
- [ ] prefix が解決できない / 存在しないときに「診断できず」になる
- [ ] 変異検証: prefix 解決をハードコードに戻すと items=0 の false green が再現することを確認する

## 対応 (2026-09-03)

**修正した。** prefix を `brew --prefix` から実測して組み立てる形にした。

- `Env` に `BrewPrefix` を足し、`expand` が **`$BREW_PREFIX`** を展開する (`$TMPDIR` と同じ規律:
  空なら error / 値は `escapeGlobMeta` で literal 化)
- カタログの `brew-orphan-state` の Paths を `/opt/homebrew/var/*` → `$BREW_PREFIX/var/*`。
  ラベルの `(/opt/homebrew/var)` も `(brew prefix の var)` へ
- `brewPrefix()` (`disk/guard.go`) を新設し、**err / rc≠0 / 空 / 非絶対パス / ルート (`/`) / Stat 失敗 /
  非ディレクトリ** をすべて error にする (fail-closed)。既定値へ fallback させると Intel Mac で
  「候補なし」という false green に化けるため
- `scanEntry` は `$BREW_PREFIX` を含むエントリだけ、guards 経由 (`g.do("prefix", ...)`) で解決してから展開する
- **`brewCleanupTargets` の相対表記も直した**: `Would remove <相対パス>` の行を prefix が取れないときに
  黙って捨てていた (= 「brew cleanup が消す対象は無い」という false green)。error を返す形にした。
  prefix の解決は遅延なので、絶対パスの行だけの出力では `brew --prefix` を呼ばない
- **同型のハードコードを `svc/report.go` でも直した** (敵対レビューの指摘)。launchd 診断の案内文言が
  `/opt/homebrew/var` を名指ししており、Intel Mac では誤った確認先を案内していた

### 変異検証

| 変異 | 結果 |
|---|---|
| カタログを `/opt/homebrew/var/*` の直書きに戻す | red (`TestCatalogHasNoHardcodedBrewPrefix`) |
| `brewPrefix` の rc≠0 検査を外す | red (`rc!=0` ケースのみ) |
| 同 空検査を外す | red (`空` ケースのみ) |
| 同 IsAbs 検査を外す | red (`相対` ケースのみ) |
| 同 ルート (`/`) 検査を外す | red (`status=ok items=13` — 実際の `/var/*` を候補にしていた) |
| 同 Stat / IsDir 検査を外す | red (`実在しない` と `ディレクトリでない` の 2 ケース) |
| `brewCleanupTargets` が相対表記を黙って捨てる旧挙動へ戻す | red (`status=ok items=0`) |

🚨 最初に書いたテーブル駆動テストは **4 ケース中 3 ケースが vacuous** だった (fixture がどれも空文字や
存在しないパスで、実際には Stat の guard 1 つが全部を弾いていた)。各ケースを「その guard だけを踏む」
形に作り直し、`r.Reason` が `"brew --prefix を取得できず"` で始まること (= 下流の `expand` /
`validateTarget` ではなく `brewPrefix` が弾いたこと) まで assert してから変異を当て直した。

### 敵対的レビュー (sonnet / read-only / 2 周)

1 周目 5 観点: 採用 2 / 却下 0。

- **採用**: 上記のテーブル駆動テストの vacuous
- **採用**: `svc/report.go` に同型のハードコードが残っている
- **壊せなかった**: `g.do("prefix", ...)` の並行性 (人工的に 2 エントリで `-race` × 20 回) /
  `brewCleanupTargets` の遅延解決 / `brew --prefix` の stdout に警告が混じる場合 (実機の
  Homebrew 6.0.21 で実測。人工的に警告を注入した 3 パターンも false green にならず) /
  prefix が symlink の場合 (`validateTarget` が拒否するので false green ではない)
- **判断材料 (劣化ではない)**: brew 未インストール環境では `brew-orphan-state` は**この変更以前から**
  `brew info` の失敗で failed だった。失敗する最初のポイントが `brew --prefix` に変わるだけで、
  新規の劣化ではない

2 周目 5 観点: 採用 1 / 却下 0。

- **採用**: `brew --prefix` が `/` を返すと `TrimRight` で空に潰れ、`$BREW_PREFIX/var/*` が `/var/*` に
  化ける (深さ 3 なので `validateTarget` の `minDepth` も素通り)。現実的なシナリオは無く、削除も未実装
  なので実害は「診断結果に紛れ込む」だけだが、④ (削除) の前に閉じておく価値があるので塞いだ
- **壊せなかった**: 4 変異のケース分離 (レビュワー側でも再現。vacuous なケースは残っていない) /
  `"."` を prefix として受け入れる経路 (`expand` の絶対パス検査が二重に防いでいることを変異で確認) /
  `svc/report.go` の文言変更が既存テストを壊す / repo 内の他のハードコード (grep 済み、無し) /
  並行性・遅延解決の再確認

### 未確認リスク / 記録

- `brew --prefix` と `brew info` が逐次で 2 回走るので、auto-update が発火する条件下では最悪ケースの
  待ち時間が構造的に増える (実測: `brew --prefix` 14ms / `brew info --json=v2 --installed` 480ms。
  per-entry timeout 60 秒に対しては十分小さい。**auto-update 発火時は未実測**)
- `brewCleanupTargets` の `resolvePrefix` は `guards.prefix` とキャッシュを共有していないので、
  両エントリが prefix を必要とする実行では `brew --prefix` が 2 回呼ばれる。現状 `brew-cleanup-residue` の
  解決は「相対表記の行があるときだけ」なので通常は発火しない。将来 `$BREW_PREFIX` を使うエントリが
  増えたら共有を検討する
- テストの `r.Reason` prefix 一致は本番側とテスト側に文言が 2 箇所書かれている。文言を変えれば test は
  red になる (サイレントに壊れる方向ではない) ので、定数化はしていない
