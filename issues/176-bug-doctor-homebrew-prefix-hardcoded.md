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
