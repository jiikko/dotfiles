# 167 bug: `collectBundleIDs` の走査漏れで、インストール済みアプリのコンテナを孤児にする

起票日: 2026-09-02
重要度: **P1**
関連: [issues/163](163-audit-doctor-implementation-red-team.md) (体 1 / 体 5) / [issues/148](148-feat-glogx-doctor-disk-diagnosis.md) (1 章 Tier 2 / 「敵対的レビュー 2 回目」の orphan-container P1)

## 対象

`src/doctor/disk/scan.go` の `collectBundleIDs` / `containerOwnedByInstalled` / `scanEntry` の GuardOrphanApp filter

## 何が起きるか

`~/Library/Containers/<...>` の孤児判定が、実在するアプリのデータコンテナを孤児 (`RiskConfirm` trash) として候補に出す。
④ (削除) の入力になるので、そのまま実装すると**生きているアプリのデータを消す候補**になる。

走査漏れは 3 系統ある (どれも `collectBundleIDs` が bundle id を集めきれない / 名前で突合できないことが原因)。

### (a) UUID 名コンテナ (iOS-on-Mac アプリ) — 実証済み

再現: 実機で `disk.Scan` を orphan-container だけの Catalog で走らせる。

- `~/Library/Containers/0A7CEF49-521F-4A65-95E2-9B8495EA27BB` が orphan と判定される
- 中身は `Data/Library/Preferences/com.jiikko.promise.plist` + `Documents/` で、実体は
  `/Applications/やくそく帳.app` (`Wrapper/Promise.app`, bundle id `com.jiikko.promise`) のデータコンテナ

iOS スタイルのコンテナは**ディレクトリ名が UUID** で、bundle id は `.com.apple.containermanagerd.metadata.plist` にしか無い。
その plist は TCC で読めない (`plutil` が operation not permitted)。`containerExcludePrefixes` の `com.jiikko.` も
`installed[id]` も**ディレクトリ名に対して**評価されるので、この形は構造的に素通りする。

### (b) `Wrapper/` と `Versions/A/Resources/` の Info.plist を読まない — 実コードで確認

`collectBundleIDs` は `<bundle>/Contents/Info.plist` しか読まない。次の 2 つの実在形が集まらない。

- iOS-on-Mac: `X.app/Wrapper/Y.app/Info.plist` (flat 配置)
- framework: `.framework/Versions/A/Resources/Info.plist` (実機に 1Password の Electron Framework 等)

(b) を直しても (a) は名前が UUID なので届かない。両方要る。

### (c) symlink の `.app` を読まない — 実証済み (実機の被害は com.apple. のみ)

`os.ReadDir` の `e.IsDir()` は symlink に対して false を返すので、symlink の `.app` は走査されない。

- 実機の depth 2 の symlink `.app` は `/Applications/Safari.app` と `Utilities/Feedback Assistant.app` の 2 件だけ
  (どちらも `com.apple.` 除外に当たるので現状は無害)
- cask は move インストールなので該当しない
- 偽 AppDirs の probe では、別所への symlink である `Linked.app` の bundle id が集まらず、そのコンテナが孤児判定された (体 5)

## 対応案

1. **fail-closed を足す**: コンテナのディレクトリ名が bundle id の形でない (UUID 正規表現に一致する) ものは
   「判定できず」にして候補から外す。metadata plist が読めるなら `MCMMetadataIdentifier` で突合し、読めなければ外す
2. `collectBundleIDs` に `Wrapper/*.app/Info.plist` と `Versions/*/Resources/Info.plist` を足す
3. `e.IsDir()` ではなく `os.Stat` (symlink 解決後) で dir 判定する。解決先が AppDirs 外でも bundle id は集める

1 が単独で被害を止めるので先に入れてよい。2 と 3 は偽陰性を減らす側。

## 受け入れ条件

- [ ] UUID 名のコンテナが候補に出ない (実機で probe を回して 0 件を確認)
- [ ] `Wrapper/` 形式の bundle id が `collectBundleIDs` に含まれる (fixture テスト)
- [ ] symlink の `.app` の bundle id が含まれる (fixture テスト)
- [ ] 変異検証: 1 の fail-closed を外すと UUID コンテナが候補に戻ることを確認する
