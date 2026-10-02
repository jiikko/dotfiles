import type { On } from 'claude-code'
import { test, expect, mock } from 'claude-code/testing'

const PROPS = { hasSurvey: false, isWorking: false, maxRows: 10, bodyColumns: 120, title: '', isFocused: false }
const OUT = '\x1b[1m~/dotfiles\x1b[0m [Opus 5.5]\n5h \x1b[32m46%\x1b[0m\n'
type Run = { argv: string[]; stdin?: string; cwd?: string }

// test の on は engine の役 (plugin の下に座る)。$ の口は { value } で包んで答える
const engineBeneath = (on: On, runs: Run[], o: { surfaces: string[]; exitCode?: number }) => {
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('turn.complete', () => ({ text: '' }))
  on('session.surfaces', () => ({ value: o.surfaces }))
  on('session.cwd', () => ({ value: '/w' }))
  on('session.model', () => ({ value: 'claude-opus-5-5' }))
  on('session.id', () => ({ value: 's1' }))
  on('session.usage', () => ({ value: { startedAt: 0, context: { tokens: 1000, window: 200000, percent: 1 }, rateLimits: [{ kind: 'five_hour', percentUsed: 46 }] } }))
  on('ui.render', ($, e) => {
    const { Box } = $.ui.resolve(e)
    return <Box key="engine-band" />
  })
  on('process.run', ($, e) => {
    runs.push({ argv: [...e.argv], stdin: typeof e.init?.stdin === 'string' ? e.init.stdin : undefined, cwd: e.init?.cwd })
    return { value: { exitCode: o.exitCode ?? 0, stdout: o.exitCode ? '' : OUT, stderr: o.exitCode ? 'boom' : '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  mock.env(on, { CLAUDE_EFFORT: 'high' })
  return mock.clock(on)
}

test('desktop のセッションでは、mod の 2 段上の statusline-command.sh を CLI と同じ形の JSON で呼ぶ', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, { surfaces: ['desktop'] })
  await $.session.start({ cwd: '/w', surface: 'desktop', isInteractive: true })
  expect(runs.length).toBe(1)
  expect(runs[0]?.argv[0]?.replace(/^.*\/_claude\//, '')).toBe('mods/desktop-statusline/../../statusline-command.sh')
  expect(runs[0]?.cwd).toBe('/w')
  const j = JSON.parse(runs[0]?.stdin ?? '{}')
  expect([j.model?.display_name, j.workspace?.current_dir, j.effort?.level, j.rate_limits?.five_hour?.used_percentage]).toEqual(['Opus 5.5', '/w', 'high', 46])
})

test('desktop でない (CLI の) セッションでは script を呼ばない', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, { surfaces: ['terminal'] })
  await $.session.start({ cwd: '/w', surface: 'terminal', isInteractive: true })
  // @ts-ignore: turn.complete の入力の型は省く
  await $.turn.complete({ reason: 'answer', answer: 'ok', durationMs: 1, isAborted: false, turnId: 't1' })
  expect(runs).toEqual([])
})

test('desktop: script の出力を色つきで帯に描く', async ($, on) => {
  engineBeneath(on, [], { surfaces: ['desktop'] })
  await $.session.start({ cwd: '/w', surface: 'desktop', isInteractive: true })
  // @ts-ignore: AbovePrompt の props の一部 (scroll / view) は使わないので省く
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'desktop', component: 'AbovePrompt', props: PROPS })
  // find の text は部分一致なので、外側の行の Text ではなく、文字がちょうど一致する区切りを選ぶ
  const exact = async (text: string) => (await ui.findAll({ type: 'Text', text })).find(el => el.text === text)
  expect((await exact('~/dotfiles'))?.props.bold).toBe(true)
  expect((await exact('46%'))?.props.color).toBe('#57ab5a')
  await ui.unmount()
})

test('terminal には描かない (CLI の statusLine と二重に出さない)', async ($, on) => {
  engineBeneath(on, [], { surfaces: ['desktop', 'terminal'] })
  await $.session.start({ cwd: '/w', surface: 'desktop', isInteractive: true })
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'terminal', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ key: 'engine-band' })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: '46%' })).toBeUndefined()
  await ui.unmount()
})

test('script が失敗したら、黙らずに理由の 1 行を出す', async ($, on) => {
  engineBeneath(on, [], { surfaces: ['desktop'], exitCode: 1 })
  await $.session.start({ cwd: '/w', surface: 'desktop', isInteractive: true })
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'desktop', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ type: 'Text', text: /ステータスバーを作れない: .*rc=1 boom/ })).toBeDefined()
  await ui.unmount()
})

test('60 秒ごとに作り直す (CLI の refreshInterval と同じ)', async ($, on) => {
  const runs: Run[] = []
  const clock = engineBeneath(on, runs, { surfaces: ['desktop'] })
  await $.session.start({ cwd: '/w', surface: 'desktop', isInteractive: true })
  await clock.advance(60_000)
  await clock.advance(60_000)
  expect(runs.length).toBe(3)
})
