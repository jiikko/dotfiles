#!/usr/bin/env bash
# scripts/golangci_ruleguard_key.rb (ruleguard の規則のファイルの中身の hash を golangci-lint の設定の disable に入れる) の契約を固定する。
# 見るもの: ruleguard の無い設定は変えない・規則の中身が変われば値が変わる・既存の disable を残す・2 度通しても重ならない・
# 当たるファイルが無い・置き換えの変数を含む rules は rc=2・glob を展開する・複数のファイルは並びに依らずどれの中身も入る・
# LANG が空のシェルでも日本語を含む設定を読む・scripts/golangci_lint.sh がこのスクリプトを通している。
# 鍵に入ることそのもの (同じキャッシュで新しい規則が効く) は golangci-lint を動かす実験で確かめた。ここでは回さない (重い)。
set -uo pipefail
unset CDPATH
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
key="$ROOT_DIR/scripts/golangci_ruleguard_key.rb"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0
ng() { printf '  ✗ %s\n' "$1"; fail=1; }

cfg() { # $1 = rules の値 / $2 = disable の値 ("" なら欄を置かない)
  ruby -E UTF-8 -rjson -e '
    rg = {"rules" => ARGV[0]}
    rg["disable"] = ARGV[1] unless ARGV[1].empty?
    puts JSON.generate({"version" => "2", "linters" => {"settings" => {"gocritic" => {"settings" => {"ruleguard" => rg}}}}})
  ' "$1" "$2"
}
disable_of() { ruby -E UTF-8 -rjson -e 'puts JSON.parse($stdin.read).dig("linters","settings","gocritic","settings","ruleguard","disable").to_s'; }
run() { (cd "$work" && ruby "$key"); }

mkdir -p "$work/gorules"
printf 'package gorules // v1\n' >"$work/gorules/rules.go"
printf 'package gorules // other\n' >"$work/gorules/more.go"

# ruleguard の無い設定は中身を変えない
plain='{"version":"2","linters":{"enable":["govet"]}}'
got=$(printf '%s' "$plain" | run) || ng "ruleguard の無い設定で落ちた"
if [ "$(printf '%s' "$got" | ruby -E UTF-8 -rjson -e 'puts JSON.generate(JSON.parse($stdin.read))')" != "$plain" ]; then
  ng "ruleguard の無い設定を書き換えた: $got"
fi

# 規則があれば disable に dotfiles-rules-<16 桁> を入れる
d1=$(cfg gorules/rules.go "" | run | disable_of)
[[ "$d1" =~ ^dotfiles-rules-[0-9a-f]{16}$ ]] || ng "disable = $d1 (dotfiles-rules-<16 桁> でない)"

# 規則の中身が変われば値が変わる (鍵が変わる) / 同じ中身なら同じ値
printf 'package gorules // v2\n' >"$work/gorules/rules.go"
d2=$(cfg gorules/rules.go "" | run | disable_of)
[ "$d1" != "$d2" ] || ng "規則の中身を変えても値が同じ: $d2"
d2b=$(cfg gorules/rules.go "" | run | disable_of)
[ "$d2" = "$d2b" ] || ng "同じ中身で値が揺れた: $d2 / $d2b"

# 既存の disable は残し、2 度通しても重ならない
d3=$(cfg gorules/rules.go "mygroup" | run | disable_of)
[ "$d3" = "mygroup,$d2" ] || ng "既存の disable を残していない: $d3 (want mygroup,$d2)"
d4=$(cfg gorules/rules.go "mygroup" | run | run | disable_of)
[ "$d4" = "mygroup,$d2" ] || ng "2 度通して重なった: $d4"

# 当たるファイルが無い・置き換えの変数を含む rules は rc=2 で止める (固定の値を hash すると鍵が中身に追いつかない)
for bad in "gorules/none.go:当たるファイルが無い" "gorules/*.txt:当たるファイルが無い" '${base-path}/gorules/rules.go:置き換える変数'; do
  out=$(cfg "${bad%%:*}" "" | run 2>&1)
  rc=$?
  { [ "$rc" = 2 ] && [[ "$out" == *"${bad#*:}"* ]]; } || ng "rules=${bad%%:*} で rc=$rc (want 2・「${bad#*:}」): $out"
done

# glob を展開し、当たったどのファイルの中身でも鍵が変わる (先頭の . のファイルも go-critic と同じく当てる)
g1=$(cfg 'gorules/*.go' "" | run | disable_of)
printf 'package gorules // more2\n' >"$work/gorules/more.go"
g2=$(cfg 'gorules/*.go' "" | run | disable_of)
[ "$g1" != "$g2" ] || ng "glob で当たった more.go を変えても値が同じ: $g2"
printf 'package gorules // v3\n' >"$work/gorules/rules.go"
g3=$(cfg 'gorules/*.go' "" | run | disable_of)
[ "$g2" != "$g3" ] || ng "glob で当たった rules.go を変えても値が同じ: $g3"
printf 'package gorules // hidden\n' >"$work/gorules/.hidden.go"
g4=$(cfg 'gorules/*.go' "" | run | disable_of)
[ "$g3" != "$g4" ] || ng "glob が先頭の . のファイルを当てない (go-critic は読む): $g4"
rm -f "$work/gorules/.hidden.go"

# 欄の名前の大文字小文字は問わない (golangci-lint と同じ)
u=$(ruby -E UTF-8 -rjson -e 'puts JSON.generate({"linters"=>{"settings"=>{"gocritic"=>{"settings"=>{"RuleGuard"=>{"Rules"=>"gorules/rules.go"}}}}}})' | run |
  ruby -E UTF-8 -rjson -e 'puts JSON.parse($stdin.read).dig("linters","settings","gocritic","settings","RuleGuard","disable").to_s')
[[ "$u" =~ ^dotfiles-rules- ]] || ng "RuleGuard / Rules の綴りで鍵が入らない: $u"
du=$(ruby -E UTF-8 -rjson -e 'puts JSON.generate({"linters"=>{"settings"=>{"gocritic"=>{"settings"=>{"ruleguard"=>{"rules"=>"gorules/rules.go","Disable"=>"mygroup"}}}}}})' | run | run |
  ruby -E UTF-8 -rjson -e 'rg = JSON.parse($stdin.read).dig("linters","settings","gocritic","settings","ruleguard"); puts [rg["Disable"], rg.key?("disable")].inspect')
[[ "$du" =~ ^\[\"mygroup,dotfiles-rules-[0-9a-f]{16}\",\ false\]$ ]] || ng "既存の Disable の欄に足していない / 2 度通して重なった: $du"

# 複数のファイルは並びに依らず、どのファイルの中身も鍵に入る
a=$(cfg "gorules/rules.go,gorules/more.go" "" | run | disable_of)
b=$(cfg "gorules/more.go, gorules/rules.go" "" | run | disable_of)
[ "$a" = "$b" ] || ng "規則のファイルの並びで値が変わった: $a / $b"
printf 'package gorules // more3\n' >"$work/gorules/more.go"
c=$(cfg "gorules/rules.go,gorules/more.go" "" | run | disable_of)
[ "$a" != "$c" ] || ng "2 つ目のファイル (more.go) を変えても値が同じ: $c"

# 呼び出し元の locale に依らず、日本語を含む設定を UTF-8 として読む (LANG・LC_ALL が空のシェルでは Ruby の既定の文字コードが US-ASCII になる)。
# 落ちるかは json の版で決まる (json 2.13 系は US-ASCII の入力を UTF-8 へ変換しようとして落ち、2.7 系は落ちない) ので、落ちるかでは見ない。
# JSON.parse に渡る文字列が UTF-8 かを、JSON.parse を包む補助 (-r) で見る (RUBYOPT の -E を引き継ぐと直しを外しても通るので外す)。入力はシェルで組む (cfg は Ruby の引数で値を受けるので、
# 引数の文字コードの扱いが混ざる)
cat >"$work/strict_json.rb" <<'R'
require "json"
JSON.singleton_class.prepend(Module.new do
  def parse(source, *args, **kw)
    # BINARY は json が UTF-8 として読むので許す ($stdin.binmode で読む直し方を赤にしない)
    raise "JSON.parse に UTF-8 でない文字列 (#{source.encoding}) が渡った" unless [Encoding::UTF_8, Encoding::BINARY].include?(source.encoding)
    super
  end
end)
R
ja='{"version":"2","linters":{"settings":{"gocritic":{"settings":{"ruleguard":{"rules":"gorules/rules.go","disable":"日本語の理由"}}}}}}'
if got=$(printf '%s' "$ja" | (cd "$work" && env -u LANG -u LC_ALL -u LC_CTYPE -u RUBYOPT ruby -r "$work/strict_json.rb" "$key")); then
  [[ "$got" == *日本語の理由* ]] || ng "LANG が空のシェルで、日本語の値が残っていない: $got"
else
  ng "LANG が空のシェルで、日本語を含む設定を読めずに落ちた"
fi

# golangci_lint.sh が合成した設定をこのスクリプトに通している (通さないと、上の契約は lint に効かない)
# 呼び出しの形で探す (コメントの中の名前に当てない)
grep -Eq '^[^#]*\| ruby "\$repo/scripts/golangci_ruleguard_key\.rb"' "$ROOT_DIR/scripts/golangci_lint.sh" ||
  ng "scripts/golangci_lint.sh が golangci_ruleguard_key.rb を通していない"

if [ "$fail" -ne 0 ]; then
  echo "✗ golangci_ruleguard_key の契約が崩れている"
  exit 1
fi
echo "✓ golangci_ruleguard_key: ruleguard の規則の中身を設定の disable に入れる (15 項目)"
