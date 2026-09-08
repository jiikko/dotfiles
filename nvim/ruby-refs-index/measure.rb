#!/usr/bin/env ruby
# frozen_string_literal: true

# 呼び出し側索引 (call-site index) が現実的かを実測するプロトタイプ。issue 334 の段階 2。
#
# なぜ: ruby-lsp の textDocument/references は索引を使わず、要求のたびにワークスペース全体を
# Prism で再パースする (requests/references.rb:63)。索引が持っているのは宣言だけで、呼び出し側の
# 逆引きが無いため。実測 11.2 秒 (ubiregi-server)。上流は Shopify/ruby-lsp#3051 を
# closed as not planned にしている。
#
# 🚨 addon では references を差し替えられない (0.26.11 で確認)。RubyLsp::Addon の公開フックは
#    code_lens / hover / document_symbol / semantic_highlighting / definition / completion /
#    discover_tests の 7 つだけで、server.rb:799 は Requests::References を直接生成する。
#    したがってこれは「ruby-lsp に組み込む実装」ではなく、**判断に必要な数字を取るための計測**。
#
# 測るもの:
#   ① 索引の構築時間      — 起動のたびに払えるコストか
#   ② メモリ増分 (RSS)    — 常駐させられるか
#   ③ クエリ時間          — 11.2 秒に対してどれだけ速いか
#   ④ ripgrep との精度差  — rg が拾う非呼び出し (コメント / 文字列 / シンボル) の件数
#
# 使い方:
#   ruby nvim/ruby-refs-index/measure.rb <workspace> <method_name> [--all-files]
#     既定は .gitignore を尊重 (rg と同じ範囲)。--all-files で ruby-lsp と同じ生 glob にする。

require "prism"
require "json"
require "open3"

# 呼び出しノードだけを集める。ruby-lsp の ReferenceFinder と同じ粒度 (名前一致) にそろえる:
# レシーバの型解析はあちらもしていないので、精度を比較する土俵を合わせるため。
class CallSiteCollector < Prism::Visitor
  #: (Hash[String, Array], Hash[String, Array], String) -> void
  def initialize(calls, defs, path)
    @calls = calls
    @defs = defs
    @path = path
    super()
  end

  def visit_call_node(node)
    loc = node.message_loc
    (@calls[node.name.to_s] ||= []) << [@path, loc.start_line, loc.start_column] if loc
    super
  end

  # 宣言も集める。references は includeDeclaration で定義行も返すので、rg と比べるときの
  # 土俵を合わせるために要る (これが無いと `def foo` の行が「rg だけに出る」に化けて、
  # rg の false positive を過大に見積もる。実測 2026-09-08 に実際そうなった)
  def visit_def_node(node)
    loc = node.name_loc
    (@defs[node.name.to_s] ||= []) << [@path, loc.start_line, loc.start_column]
    super
  end
end

module RefsIndex
  module_function

  #: -> Integer  (KB)
  def rss_kb
    `ps -o rss= -p #{Process.pid}`.to_i
  end

  #: (String, bool) -> Array[String]
  def ruby_files(workspace, all_files)
    if all_files
      # ruby-lsp の references.rb:63 と同じ生 glob
      Dir.glob(File.join(workspace, "**/*.rb"))
    else
      # rg と同じ範囲 (.gitignore 尊重) にそろえる。比較の土俵を合わせるため
      # 🚨 パスを明示する。rg はパス引数が無く stdin が tty でないと **stdin を読む**ので、
      #    Open3 越しだと 0 バイト検索になって黙って 0 件を返す (実測 2026-09-08)。
      out, status = Open3.capture2("rg", "--files", "--type", "ruby", ".", chdir: workspace)
      raise "rg --files に失敗した (rc=#{status.exitstatus})" unless status.success?

      out.lines(chomp: true).map { |rel| File.expand_path(rel, workspace) }
    end
  end

  #: (Array[String]) -> [Hash[String, Array], Float, Integer]
  def build(paths)
    calls = {}
    defs = {}
    before = rss_kb
    t = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    paths.each do |path|
      result = Prism.parse_file(path)
      result.value.accept(CallSiteCollector.new(calls, defs, path))
    rescue StandardError
      # 壊れたファイルは飛ばす (ruby-lsp も rescue して続ける)
      next
    end
    elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC) - t
    [calls, defs, elapsed, rss_kb - before]
  end

  #: (String, String) -> Array[[String, Integer]]
  def ripgrep_hits(workspace, name)
    # パスの明示は上と同じ理由 (無いと stdin を読む)
    out, status = Open3.capture2("rg", "-w", "--type", "ruby", "--json", name, ".", chdir: workspace)
    return [] unless status.success?

    out.lines.filter_map do |line|
      event = JSON.parse(line)
      next unless event["type"] == "match"

      data = event["data"]
      [File.expand_path(data["path"]["text"], workspace), data["line_number"]]
    end
  end

  #: (Array[String]) -> void
  def run(argv)
    workspace = File.expand_path(argv[0] || Dir.pwd)
    name = argv[1] or abort("usage: measure.rb <workspace> <method_name> [--all-files]")
    all_files = argv.include?("--all-files")

    paths = ruby_files(workspace, all_files)
    puts "workspace : #{workspace}"
    puts "対象       : #{paths.size} 件 (#{all_files ? 'ruby-lsp と同じ生 glob' : 'rg と同じ範囲 (.gitignore 尊重)'})"

    calls, defs, build_sec, rss_delta = build(paths)
    puts format("① 構築時間 : %.2f 秒", build_sec)
    puts format("② メモリ増分: %.1f MB (RSS)", rss_delta / 1024.0)
    puts format("   索引サイズ: 呼び出し %d 種 / %d 箇所、宣言 %d 種 / %d 箇所",
                calls.size, calls.values.sum(&:size), defs.size, defs.values.sum(&:size))

    t = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    hits = (calls[name] || []) + (defs[name] || [])
    query_sec = Process.clock_gettime(Process::CLOCK_MONOTONIC) - t
    puts format("③ クエリ    : %.6f 秒 (呼び出し %d + 宣言 %d)  ← ruby-lsp は 11.2 秒",
                query_sec, (calls[name] || []).size, (defs[name] || []).size)

    # ④ 精度差。rg のヒットのうち、AST 上は呼び出しでも宣言でもない行が「本当のゴミ」
    rg = ripgrep_hits(workspace, name)
    ast_lines = hits.map { |path, line, _| [path, line] }.to_set
    rg_only = rg.reject { |pair| ast_lines.include?(pair) }
    ast_only = ast_lines.reject { |pair| rg.include?(pair) }
    puts format("④ 精度      : rg %d 行 / AST %d 箇所", rg.size, hits.size)
    puts format("   rg だけに出る行 (コメント・文字列・シンボル等の誤検出): %d", rg_only.size)
    puts format("   AST だけに出る箇所 (同一行に複数、など): %d", ast_only.size)
    rg_only.first(5).each { |path, line| puts "     - #{path.delete_prefix(workspace + '/')}:#{line}" }
  end
end

require "set"
RefsIndex.run(ARGV) if $PROGRAM_NAME == __FILE__
