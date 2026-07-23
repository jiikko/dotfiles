# 022 feat(glogx): `--json` 出力 (優先度低・保留気味)

## 背景

README「未対応 (必要になったら issue 化)」に挙がっていた項目。スクリプトや Claude Code からの機械利用向けに、コミット + CI 状態 + PR を JSON で吐く静的モード。

**ただし 2026-07-23 の会話で、ユーザーの glogx 利用は ctrl+g popup 経由のみと判明。シェルから直接叩く用途が現状無いため、具体的な消費者 (どのスクリプト / どのワークフローが読むか) が現れるまで着手しない。**着手前にこの前提を再確認すること。

## やること (着手時)

- `glogx --json` で対話ブラウズせず JSON を stdout へ 1 回出力 (`--no-pager` の JSON 版。両立不可の引数組み合わせはエラー)
- スキーマ案:

  ```json
  {
    "commits": [
      {
        "sha": "...", "subject": "...", "author": "...", "date": "...",
        "ci": {"state": "failure", "jobs": [{"name": "lint", "state": "failure", "url": "...", "duration_sec": 13}]},
        "pr": {"number": 123, "state": "OPEN", "url": "..."}
      }
    ],
    "warnings": ["gh が未認証のため..."]
  }
  ```

- CI 取得失敗は `state: "unknown"` + warnings 配列 (終了コード 0 の既存方針を踏襲)
- jobs はコミット一覧の一括取得に含まれる範囲 (name + 集約状態)。詳細 (steps/annotations) は含めない

## 関連

- README の未対応リストからこの issue へ参照を張り替える (着手時)
