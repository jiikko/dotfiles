#!/usr/bin/env ruby
# frozen_string_literal: true

# golangci_ruleguard_key.rb — 合成した golangci-lint の設定 (stdin の JSON) に、ruleguard の規則のファイルの中身の hash を入れて stdout に出す。
# scripts/golangci_lint.sh が golangci_config.rb の後に通す。規則のファイルのパスは cwd (モジュールの根) から読む (golangci-lint と同じ)。
#
# なぜ: ruleguard の規則のファイルは設定からパスで指されるだけで、中身が golangci-lint の結果キャッシュの鍵に入らない。手元では、規則を
# 足してもキャッシュ済みのパッケージに新しい規則が効かず黙って緑になり、規則の読み込みに失敗した結果 (`empty rule set`) も、規則を
# 直した後まで残る (v2.15.0 で実測)。鍵に入るのは linter の設定なので、ruleguard の設定の disable に、存在しない group 名
# `dotfiles-rules-<hash>` として入れる (どの規則も外さない)。
# 設定のトップに知らない欄を足す形にしない: golangci-lint はエラーにしないが、鍵にも入れない (同じ実験で、値を変えても古い結果が返った)。
#
# rules は go-critic と同じく glob として展開する (`gorules/*.go` も書ける)。次は rc=2 で止める (固定の値を hash すると、鍵が中身に
# 追いつかず、規則を変えても効かない状態に黙って戻る): golangci-lint が置き換える変数 (`${base-path}` など) を含む・1 つも当たらない。
# 検出しない形: 規則のファイルが dsl.ImportRules で取り込む別の規則の束 (その中身は鍵に入らない。今の規則はどれも dsl しか import しない)
require "digest"
require "json"

TAG = "dotfiles-rules-"

# 欄の名前は大文字小文字を区別せずに引く (golangci-lint の設定の読み込み (viper) と同じ。`RuleGuard:` でも効くのに鍵が抜けないように)
def fetch(h, name)
  return nil unless h.is_a?(Hash)

  k = h.keys.find { |key| key.to_s.casecmp?(name) }
  k && h[k]
end

j = JSON.parse($stdin.read)
rg = %w[linters settings gocritic settings ruleguard].reduce(j) { |h, name| fetch(h, name) }
rules_key = rg.is_a?(Hash) && rg.keys.find { |k| k.casecmp?("rules") }
if rules_key && rg[rules_key].is_a?(String) && !rg[rules_key].empty?
  d = Digest::SHA256.new
  rg[rules_key].split(",").map(&:strip).reject(&:empty?).sort.each do |pattern|
    if pattern.include?("${")
      warn "golangci_ruleguard_key: rules の #{pattern.inspect} は golangci-lint が置き換える変数を含む (中身を鍵に入れられない。モジュールの根からの相対パスか glob で書く)"
      exit 2
    end
    # 先頭の . も * で当てる (go-critic の filepath.Glob と同じ。Ruby の既定は当てず、go-critic が読むファイルが鍵から抜ける)
    files = Dir.glob(pattern, File::FNM_DOTMATCH).select { |f| File.file?(f) }.sort
    if files.empty?
      warn "golangci_ruleguard_key: rules の #{pattern.inspect} に当たるファイルが無い (#{Dir.pwd} から探した)"
      exit 2
    end
    files.each { |f| d << f << "\0" << File.binread(f) << "\0" }
  end
  tag = TAG + d.hexdigest[0, 16]
  disable_key = rg.keys.find { |k| k.casecmp?("disable") } || "disable"
  groups = rg[disable_key].to_s.split(",").map(&:strip).reject { |g| g.empty? || g.start_with?(TAG) }
  rg[disable_key] = (groups + [tag]).join(",")
end
puts JSON.pretty_generate(j)
