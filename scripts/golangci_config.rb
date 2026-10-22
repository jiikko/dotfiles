#!/usr/bin/env ruby
# frozen_string_literal: true

# golangci_config.rb BASE [LOCAL] — golangci-lint の設定を、共通の塊 (src/.golangci.yml) とモジュールの差分
# (src/<module>/.golangci.local.yml) から合成して stdout に JSON で出す。scripts/golangci_lint.sh が run のたびに呼び、
# 中身の sha256 で決まるファイル (.json) に書いて -c で渡す。YAML で書き出さない: ruby の版で引用の付け方が変わり、同じ入力から同じバイト列に
# ならない (2.6 と 3.2 で実測)。JSON は版で書き方が揺れない (合成した設定は commit しない。写しを置くと、ずれを止めたい二重管理が残る)。
#
# 合成の規則 (同じ入力からは常に同じバイト列を出す):
#   - 読むのは引数の 2 つだけで、この順 (ベース → 差分)。glob で探さない
#   - map は鍵ごとに再帰的に合わせる。一覧は、ベースの要素の後に、差分の要素のうちベースに無いものを差分の順に足す (同じ要素を 2 度入れない)
#   - 出力の鍵の並びは、ベースの順、その後に差分だけにある鍵を差分の順
#   - 🚨 差分は足すだけ。ベースにある値を別の値で書き換える・型の違う値で上書きする (`linters:` を空にする・一覧を文字列にする) と
#     rc=2 で落とす。ベースを外せる鍵 (DENY) も落とす。どれも golangci-lint はエラーにせず、共通の linter が黙って消えて緑になる
#   - YAML は文書が 1 つで、同じ map に同じ鍵が 2 度無いことを確かめてから読む (ruby の YAML は後の鍵で黙って上書きするが、
#     golangci-lint は同じ形をエラーにしていた。合成で緑にしない)
#
# golangci-lint 自体の extends / include は使えない: v2.15.0 は書いてもエラーも警告も出さずに無視し、既定の linter を有効にする
# (実測 2026-10-22)。
require "json"
require "yaml"

def die(msg)
  warn "golangci_config: #{msg}"
  exit 2
end

# ベースを外せる鍵 (差分に書かせない)。default は既定の linter の集合を変え、disable は共通の linter を外す
DENY = [%w[linters default], %w[linters disable], %w[formatters disable]].freeze

def check_unique_keys(node, path)
  if node.is_a?(Psych::Nodes::Mapping)
    seen = {}
    node.children.each_slice(2) do |k, v|
      key = k.respond_to?(:value) ? k.value : k.to_s
      die "#{path}: 同じ鍵 #{key.inspect} が 2 度ある (行 #{k.start_line + 1})" if seen.key?(key)
      seen[key] = true
      check_unique_keys(v, path)
    end
  else
    Array(node.children).each { |c| check_unique_keys(c, path) }
  end
end

def load_map(path)
  text = File.read(path)
  docs = Psych.parse_stream(text).children
  die "#{path}: YAML の文書が #{docs.size} 個ある (1 つだけにする)" if docs.size > 1
  docs.each { |d| check_unique_keys(d, path) }
  data = YAML.safe_load(text, aliases: false)
  data = {} if data.nil?
  die "#{path}: 最上位が map でない" unless data.is_a?(Hash)
  data
rescue Errno::ENOENT
  die "#{path}: 無い"
rescue Psych::Exception => e
  die "#{path}: YAML として読めない (#{e.message})"
end

# パターンマッチ (case/in) は書かない: macOS の /usr/bin/ruby は 2.6 で、CI の runner がそれを引いても動くようにする。
def merge(base, local, path = [])
  if base.is_a?(Hash) && local.is_a?(Hash)
    out = {}
    base.each { |k, v| out[k] = local.key?(k) ? merge(v, local[k], path + [k]) : v }
    local.each do |k, v|
      next if base.key?(k)
      die "差分に #{(path + [k]).join('.')} を書かない (共通の塊の linter を外せる)" if DENY.include?(path + [k])
      out[k] = v
    end
    out
  elsif base.is_a?(Array) && local.is_a?(Array)
    base + local.reject { |x| base.include?(x) }
  elsif base == local
    base
  else
    die "差分が共通の塊の #{path.join('.')} を書き換えている (#{base.inspect[0, 60]} → #{local.inspect[0, 60]})。差分は足すだけにする"
  end
end

die 'usage: golangci_config.rb BASE [LOCAL]' unless [1, 2].include?(ARGV.size)
base = load_map(ARGV[0])
local = ARGV[1] ? load_map(ARGV[1]) : {}
print JSON.pretty_generate(merge(base, local)), "\n"
