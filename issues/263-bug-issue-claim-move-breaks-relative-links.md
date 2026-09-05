# 263 bug: issue を `next/` へ claim すると本文の相対リンクが切れ、doc-links gate が push を止める (glogx の `n` も同じ)

起票日: 2026-09-05

## 観測 (obaket、2026-09-05)

`git mv issues/634-*.md issues/next/` で claim した直後の push が、pre-push の `check-doc-links` で落ちた:

```
BROKEN LINK: issues/next/634-bug-per-file-upload-reject-reason-missing-from-logs.md
  link → done/549-bug-readonly-upload-reject-lacks-provider-context.md
  link → 569-bug-finder-drop-rejected-as-nousablefiles.md
```

issue 本文の相対リンク (`done/549-...` / `569-...`) は `issues/` 直下を起点に書かれており、1 段下の `next/` へ動くと全部切れる。
同日 635 でも同じことが起き、どちらも手で `../` を足した。glogx の issues viewer の `n` (`src/glogx/issues/move.go:MoveToSubdir`) は
ファイルを rename するだけで本文を書き換えないので、人が `n` で claim しても同じ壊れ方をする (その場合 gate に落ちるのは次に push する人)。

`done/` への移動でも同じ問題があるが、こちらは done 移動の規律 (`issue-done-on-completion.md`) が「参照パスを grep して同期」を要求しているので
運用で吸収されている。claim は「移動だけを即 push」する規律なので、リンクの書き換えが挟まりにくい。

## 前提 (codex 反証レビュー 2026-09-05 で確認)

- `MoveToSubdir` (`src/glogx/issues/move.go:49`) は `os.Rename` だけで本文を読まない
- obaket の gate `macOS/bin/check-doc-links:38` はリンクを **その md の所在ディレクトリ基準**で解決し、`:18` で `done/` 配下は検査対象から除外している
  (= done 移動で切れたリンクは gate に見えない。next / pending 移動だけが gate で見える)
- `next-claim-push.sh` は Bash の `git mv` を静的検査する hook で、glogx の `n` は通らない。commit / push を案内するだけで doc-links は回さない

## 対応案 (どれか)

- A: `MoveToSubdir` が移動時に本文の相対リンク (`](path.md)` で repo 内を指すもの) を移動先起点に書き換える。
  移動元と移動先の**両方の深さ**から相対パスを再計算する (global `next/` だけでなく `epic/<name>/next/` のような group 配下も対象)
- B: issue 内リンクの起点を「issue ディレクトリ直下」に固定する規約にして、gate 側で状態 dir (`next/` `pending/` `done/`) と group の `next/` 配下は
  親を起点に解決する。書き換え不要だが、gate が done を除外している現状と整合させる方針を先に決める
- C: claim の規律 (`claim-issue-in-next-and-push.md`) に「移動後に `check-doc-links` を回してリンクを直す」を足す (運用で吸収。`n` 経由は hook で
  拾えないので規律のみ)

A (glogx) + C (規律) で Claude / 人の両経路を覆う。B は gate の解決規則を変えるので obaket 側で別判断。

## 関連

- obaket `issues/721-retro-634-635-codex-drive-2026-09-05.md` 反省 7
- `_claude/rules/claim-issue-in-next-and-push.md` / `src/glogx/issues/move.go`
