import { atom, read, update } from 'claude-code'
import type { Register, EngineInterface } from 'claude-code'
import { parseCounts, segments } from './band'

// human / retro の催促を、モデル経由でなく人へ直接、プロンプトの上の帯に出す (issue 621)。
// 数えるのは settings の SessionStart の hook と同じ script の --counts。帯が出なくても SessionStart の注入と規約の「冒頭で一言伝える」は
// しばらく残す (mod は失敗すると黙ってスキップされるので、帯だけに頼らない。issue 618「mod が黙って止まったときの扱い」)。
// 対話のセッションだけで数える: user の settings を読む裏の `claude -p` (ratelimit の `claude -p /usage` 等) でも mods は走る (docs/claude-mods.md)。

const counts = atom({ plugin: 'issue-band', key: 'counts' } as const, null)

const refresh = async ($: EngineInterface, cwd: string) => {
  const hooks = `${$.plugin.root}/../../hooks`
  const init = { cwd, stdin: JSON.stringify({ cwd }), timeoutMs: 10_000 }
  try {
    const [human, retro] = await Promise.all([
      $.process.run([`${hooks}/human-tasks-due.sh`, '--counts'], init),
      $.process.run([`${hooks}/retro-open.sh`, '--counts'], init),
    ])
    // 非 0 で stdout が空だと「issue dir の無い repo」と同じに見えて帯が黙って消えるので、理由として出す
    const failed = [human, retro].find(r => r.exitCode !== 0)
    if (failed) throw new Error(`rc=${failed.exitCode} ${failed.stderr.trim().slice(0, 80)}`)
    const next = parseCounts(human.stdout, retro.stdout)
    await update($, counts, () => next)
  } catch (err) {
    await update($, counts, () => ({ overdue: 0, soon: 0, broken: 0, retro: 0, error: String(err).slice(0, 120) }))
  }
}

// 対話かどうかは毎回聞く (module の変数は、保存や pull で module が読み直されると消える)
const isInteractive = async ($: EngineInterface) => (await $.session.surfaces()).length > 0

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    if (e.isInteractive) await refresh($, e.cwd)
    return started
  })

  // issue を done へ移した直後に減るよう、メインのターンの終わりにも数え直す (描画の中では script を呼ばない)。
  // サブエージェントのターン (agentId あり) では数えない。待たずに裏で走らせる (script は issue の数だけ時間がかかり、dotfiles で約 0.6 秒)
  on('turn.complete', async ($, e, next) => {
    const done = await next(e)
    if (e.agentId === undefined && (await isInteractive($))) void refresh($, await $.session.cwd())
    return done
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const c = await read($, counts)
    const parts = c === null || e.props.hasSurvey ? [] : segments(c)
    if (parts.length === 0) return next(e)
    const { Box, Text } = $.ui.resolve(e)
    return (
      <Box>
        {parts.map((p, i) => (
          <Text key={`seg${i}`} wrap="truncate">
            {i > 0 ? <Text dimColor> · </Text> : null}
            <Text color={p.color}>{p.text}</Text>
          </Text>
        ))}
      </Box>
    )
  })
}
