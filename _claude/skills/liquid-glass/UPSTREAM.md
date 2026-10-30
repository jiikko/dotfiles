# liquid-glass の出典

この skill は外部の公開 repo から取り込んだもの。自分で書いた skill ではないので、本文を直したくなったら先に upstream の版を見る。

- repo: https://github.com/rshankras/claude-code-apple-skills
- path: `skills/design/liquid-glass/SKILL.md`
- commit: `9ffb83138209057875698dd11c1720c657c47a92` (取り込んだ日: 2026-10-30。SKILL.md の frontmatter は `last_verified: 2026-07-16` / `os_version: iOS 27 / macOS 27`)
- license: MIT (`LICENSE` は upstream の repo の LICENSE をそのまま写した。MIT は再配布に著作権表示と許諾文の同梱を求める)

## 手を入れた所

- frontmatter の `description` の末尾に、日本語の依頼で発火させるための語を足した (「リキッドグラス」など)。本文は upstream のまま

## 選んだ理由 (2026-10-30 に比べた候補)

| 候補 | 採らなかった理由 |
|---|---|
| Dimillian/Skills の `swiftui-liquid-glass` (MIT) | iOS の例だけで、macOS (AppKit・ツールバー) と WWDC25 の設計の規則 (ガラスは navigation の層だけ・ガラスにガラスを重ねない) が無い |
| haider-nawaz/liquid-glass-skill | macOS を扱うが license が無い (公開の dotfiles に写せない) |
| e-palmisano/macos-dev (MIT) | macOS 開発全般の大きな参照集 (Liquid Glass の文書だけで 93 KB) で、Liquid Glass だけを引く skill にならない |

## 更新するとき

upstream の同じ path を取り直し、上の「手を入れた所」をもう一度当てて、commit をこの文書に書き直す。

```sh
gh api 'repos/rshankras/claude-code-apple-skills/contents/skills/design/liquid-glass/SKILL.md?ref=<commit>' --jq .content | base64 -d
```
