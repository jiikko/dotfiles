import { atom, read, update } from 'claude-code'
import type { Register, EngineInterface } from 'claude-code'
import { parseAnsi } from './ansi'
import { statusInput } from './input'

// Claude desktop の Code タブは settings の statusLine を実行しない (issue 625 で観測) ので、CLI と同じステータスバーを mod で描く。
// 中身は CLI と同じ _claude/statusline-command.sh の出力 (表示の正本は script 1 つ。色の閾値も script が決め、ここは ANSI を読むだけ)。
// desktop の surface にだけ描き、terminal では描かない (CLI は statusLine が描くので二重に出さない)。
// script は描画の中では呼ばない: セッションの開始・メインのターンの終わり・60 秒ごと (CLI の refreshInterval と同じ) に呼んで $.state に置く。
// mod が読み込まれない・script が失敗したときは、帯が出ない / 理由の 1 行が出る (CLI の statusLine には影響しない)。
//
// 「desktop のセッションか」は $.session.surfaces() でなく、起動時に決まる環境変数で聞く。desktop が起こすセッションは `-p` / SDK と同じ
// 作りで、型定義 (SessionStartInput / session.attach) によると session.start の時点では surface が null・名簿が空で、desktop は
// 最初の描画の要求で名簿に載る。名簿で聞くと、session.start の refresh も 60 秒ごとの timer も起動されず、帯は一度も作られない
// (issue 625 / 628 で「desktop に何も出ない」が続いた原因の最有力。desktop 実機の session.start の surface は未観測)。
// 描くかどうかは描画のたびに e.surface で決める (名簿でなく、描く先そのもの)。

const lines = atom({ plugin: 'desktop-statusline', key: 'lines' } as const, null)
const error = atom({ plugin: 'desktop-statusline', key: 'error' } as const, null)

const isDesktopHosted = async ($: EngineInterface) => (await $.env.get('CLAUDE_CODE_ENTRYPOINT')) === 'claude-desktop'

const refresh = async ($: EngineInterface) => {
  try {
    const [cwd, modelId, usage, sessionId, effort] = await Promise.all([
      $.session.cwd(),
      $.session.model(),
      $.session.usage(),
      $.session.id(),
      $.env.get('CLAUDE_EFFORT'),
    ])
    const stdin = JSON.stringify(statusInput({ cwd, modelId, usage, effort, sessionId }))
    const r = await $.process.run([`${$.plugin.root}/../../statusline-command.sh`], { cwd, stdin, timeoutMs: 10_000 })
    if (r.exitCode !== 0) throw new Error(`rc=${r.exitCode} ${r.stderr.trim().slice(0, 80)}`)
    if (r.stdout.trim() === '') throw new Error('script の出力が空')
    const parsed = parseAnsi(r.stdout)
    await update($, lines, () => parsed)
    await update($, error, () => null)
  } catch (err) {
    await update($, error, () => String(err).slice(0, 160))
  }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    if (await isDesktopHosted($)) {
      await refresh($)
      $.clock.every(60_000, () => refresh($))
    }
    return started
  })

  on('turn.complete', async ($, e, next) => {
    const done = await next(e)
    if (e.agentId === undefined && (await isDesktopHosted($))) void refresh($)
    return done
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    if (e.surface !== 'desktop' || e.props.hasSurvey) return next(e)
    const ls = await read($, lines)
    const err = await read($, error)
    if (ls === null && err === null) return next(e)
    const { Box, Text } = $.ui.resolve(e)
    return (
      <Box flexDirection="column">
        {(ls ?? []).map((segs, i) => (
          <Text key={`line${i}`}>
            {segs.map((s, j) => (
              <Text key={`s${i}-${j}`} color={s.color} backgroundColor={s.backgroundColor} bold={s.bold} underline={s.underline}>
                {s.text}
              </Text>
            ))}
          </Text>
        ))}
        {err !== null ? <Text key="error" dimColor>{`ステータスバーを作れない: ${err}`}</Text> : null}
      </Box>
    )
  })
}
