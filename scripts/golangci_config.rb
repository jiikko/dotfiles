#!/usr/bin/env ruby
# frozen_string_literal: true

# golangci_config.rb BASE [LOCAL] — golangci-lint の設定を、共通の塊 (src/.golangci.yml) とモジュールの差分
# (src/<module>/.golangci.local.yml) から合成して stdout に JSON で出す。scripts/golangci_lint.sh が run のたびに呼び、
# 一時ファイル (.json) に書いて -c で渡す。YAML で書き出さない: ruby の版で引用の付け方が変わり、同じ入力から同じバイト列に
# ならない (2.6 と 3.2 で実測)。JSON は版で書き方が揺れない (合成した設定は commit しない。写しを置くと、ずれを止めたい二重管理が残る)。
#
# 合成の規則 (同じ入力からは常に同じバイト列を出す):
#   - 読むのは引数の 2 つだけで、この順 (ベース → 差分)。glob で探さない
#   - map は鍵ごとに再帰的に合わせる。両方が map でない同じ鍵は、差分の値で置き換える
#   - 一覧は、ベースの要素の後に、差分の要素のうちベースに無いものを差分の順に足す (同じ要素を 2 度入れない)
#   - 出力の鍵の並びは、ベースの順、その後に差分だけにある鍵を差分の順
#   - 差分の version はベースと同じでなければ落とす (別の版の書式を混ぜない)
#
# golangci-lint 自体の extends / include は使えない: v2.15.0 は書いてもエラーも警告も出さずに無視し、既定の linter を有効にする
# (実測 2026-10-22)。
require "json"
require "yaml"

def die(msg)
  warn "golangci_config: #{msg}"
  exit 2
end

def load_map(path)
  data = YAML.safe_load(File.read(path), aliases: false)
  data = {} if data.nil?
  die "#{path}: 最上位が map でない" unless data.is_a?(Hash)
  data
rescue Errno::ENOENT
  die "#{path}: 無い"
rescue Psych::Exception => e
  die "#{path}: YAML として読めない (#{e.message})"
end

# パターンマッチ (case/in) は書かない: macOS の /usr/bin/ruby は 2.6 で、CI の runner がそれを引いても動くようにする。
def merge(base, local)
  if base.is_a?(Hash) && local.is_a?(Hash)
    out = {}
    base.each { |k, v| out[k] = local.key?(k) ? merge(v, local[k]) : v }
    local.each { |k, v| out[k] = v unless base.key?(k) }
    out
  elsif base.is_a?(Array) && local.is_a?(Array)
    base + local.reject { |x| base.include?(x) }
  else
    local
  end
end

die 'usage: golangci_config.rb BASE [LOCAL]' unless [1, 2].include?(ARGV.size)
base = load_map(ARGV[0])
local = ARGV[1] ? load_map(ARGV[1]) : {}
if local.key?('version') && local['version'] != base['version']
  die "#{ARGV[1]}: version #{local['version'].inspect} がベースの #{base['version'].inspect} と違う"
end
print JSON.pretty_generate(merge(base, local)), "\n"
