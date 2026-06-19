#!/bin/sh
unset CDPATH
# @DotfilesSyncer:name Karabiner 設定を復元
# @DotfilesSyncer:description dotfiles の Karabiner 設定を復元してキーボード種別を自動設定

src=~/dotfiles/mac/karabiner.json
target=~/.config/karabiner/karabiner.json

cp "$src" "$target"

# 既知の外部キーボード → レイアウト対応表。
# キーは "VendorID ProductID"（10進）。値は算術評価で正規化してから渡すので、
# hidutil の出力が 16進(0x29ea)でも10進でも、macOS バージョン差を問わず一致する。
# サードパーティ製キーボードは ioreg の KeyboardLanguage を報告しないため、
# 内蔵キーボードの言語自動判定では拾えない（常に内蔵 JIS が勝ってしまう）。
# VendorID/ProductID で明示的に対応付ける。新しい外部キーボードを使うときは
# `hidutil list` で値を調べ（例: 0x29ea/0x360 → 10進 10730/864）ここに1行追加する。
known_external_layout() {
    case "$1" in
        "10730 864") echo ansi ;;  # Kinesis Advantage360 (ANSI)
        *)           echo "" ;;
    esac
}

# 内蔵キーボードの言語から判定する（KeyboardLanguage を報告するのは Apple 製のみ）。
internal_layout() {
    kb_lang=$(ioreg -r -k KeyboardLanguage 2>/dev/null | grep '"KeyboardLanguage"' | head -1)
    if echo "$kb_lang" | grep -qi "Japanese"; then
        echo jis
    elif echo "$kb_lang" | grep -qi "ISO"; then
        echo iso
    else
        echo ansi
    fi
}

# 接続中の HID デバイスを列挙し、既知の外部キーボードが見つかればそのレイアウトを
# 優先する。無ければ内蔵キーボードの言語にフォールバック。対応表のキーは
# VendorID+ProductID で一意に外部キーボードを特定するため、UsagePage/Usage での
# 絞り込みは不要（同じ VendorID/ProductID の別インターフェース行に当たっても問題ない）。
detect_layout() {
    while read -r vid pid _rest; do
        # 数値でない行（ヘッダ等）はスキップ
        case "$vid" in 0x*|[0-9]*) ;; *) continue ;; esac
        case "$pid" in 0x*|[0-9]*) ;; *) continue ;; esac
        layout=$(known_external_layout "$(( vid )) $(( pid ))")
        if [ -n "$layout" ]; then
            echo "$layout"
            return
        fi
    done <<EOF
$(hidutil list 2>/dev/null)
EOF
    internal_layout
}

layout=$(detect_layout)

case "$layout" in
    jis) country_code=45 ;;
    iso) country_code=13 ;;
    *)
        # ansi: ベースの karabiner.json が既に ansi なのでパッチ不要
        echo 'dotfilesにあるkarabinerの設定ファイルをkarabinerにコピーしました。(ansi)'
        exit 0
        ;;
esac

jq --arg type "$layout" --argjson code "$country_code" \
    '.profiles[].virtual_hid_keyboard.keyboard_type = $type
     | .profiles[].virtual_hid_keyboard.keyboard_type_v2 = $type
     | .profiles[].virtual_hid_keyboard.country_code = $code' \
    "$target" > "${target}.tmp" && mv "${target}.tmp" "$target"

echo "dotfilesにあるkarabinerの設定ファイルをkarabinerにコピーしました。(${layout}に自動設定)"
