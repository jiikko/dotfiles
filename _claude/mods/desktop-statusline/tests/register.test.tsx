import type { On } from 'claude-code'
import { test, expect, mock } from 'claude-code/testing'

const PROPS = { hasSurvey: false, isWorking: false, maxRows: 10, bodyColumns: 120, title: '', isFocused: false }
const OUT = '\x1b[1m~/dotfiles\x1b[0m [Opus 5.5]\n5h \x1b[32m46%\x1b[0m\n'
type Run = { argv: string[]; stdin?: string; cwd?: string }
type World = { surfaces: string[]; entrypoint?: string; exitCode?: number; stdout?: string; commands?: { name: string; immediate?: boolean }[] }

// desktop が起こしたセッションの開始: 型定義 (SessionStartInput) では `-p` / SDK と同じく、まだ何も描いておらず surface は null・名簿は空
const DESKTOP_START = { cwd: '/w', surface: null, isInteractive: false } as const
const DESKTOP: World = { surfaces: [], entrypoint: 'claude-desktop' }
const CLI_START = { cwd: '/w', surface: 'terminal', isInteractive: true } as const
const CLI: World = { surfaces: ['terminal'], entrypoint: 'cli' }
const TURN = { reason: 'answer', answer: 'ok', durationMs: 1, isAborted: false, turnId: 't1' } as const

// test の on は engine の役 (plugin の下に座る)。$ の口は { value } で包んで答える
const engineBeneath = (on: On, runs: Run[], o: World) => {
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('turn.complete', () => ({ text: '' }))
  on('session.surfaces', () => ({ value: o.surfaces }))
  on('command.register', ($, e) => {
    o.commands?.push({ name: e.name, immediate: e.immediate })
    return { value: { command: e.name } }
  })
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
    const stdout = o.exitCode ? '' : (o.stdout ?? OUT)
    return { value: { exitCode: o.exitCode ?? 0, stdout, stderr: o.exitCode ? 'boom' : '', isStdoutTruncated: false, isStderrTruncated: false } }
  })
  mock.env(on, { CLAUDE_EFFORT: 'high', ...(o.entrypoint ? { CLAUDE_CODE_ENTRYPOINT: o.entrypoint } : {}) })
  return mock.clock(on)
}

test('desktop が起こしたセッションは、開始時に surface が null・名簿が空でも、mod の 2 段上の statusline-command.sh を CLI と同じ形の JSON で呼ぶ', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, DESKTOP)
  await $.session.start(DESKTOP_START)
  expect(runs.length).toBe(1)
  expect(runs[0]?.argv[0]?.replace(/^.*\/_claude\//, '')).toBe('mods/desktop-statusline/../../statusline-command.sh')
  expect(runs[0]?.cwd).toBe('/w')
  const j = JSON.parse(runs[0]?.stdin ?? '{}')
  expect([j.model?.display_name, j.workspace?.current_dir, j.effort?.level, j.rate_limits?.five_hour?.used_percentage]).toEqual(['Opus 5.5', '/w', 'high', 46])
})

test('desktop でない (CLI の) セッションでは script を呼ばない', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, CLI)
  await $.session.start(CLI_START)
  // @ts-ignore: turn.complete の入力の型は省く
  await $.turn.complete(TURN)
  expect(runs).toEqual([])
})

test('非対話 (claude -p) のセッションでは script を呼ばない', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, { surfaces: [] })
  await $.session.start(DESKTOP_START)
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  expect(runs).toEqual([])
})

test('desktop: script の出力を色つきで帯に描く', async ($, on) => {
  engineBeneath(on, [], DESKTOP)
  await $.session.start(DESKTOP_START)
  // @ts-ignore: AbovePrompt の props の一部 (scroll / view) は使わないので省く
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'desktop', component: 'AbovePrompt', props: PROPS })
  // find の text は部分一致なので、外側の行の Text ではなく、文字がちょうど一致する区切りを選ぶ
  const exact = async (text: string) => (await ui.findAll({ type: 'Text', text })).find(el => el.text === text)
  expect((await exact('~/dotfiles'))?.props.bold).toBe(true)
  expect((await exact('46%'))?.props.color).toBe('#57ab5a')
  await ui.unmount()
})

test('terminal には描かない (CLI の statusLine と二重に出さない)', async ($, on) => {
  engineBeneath(on, [], { ...DESKTOP, surfaces: ['desktop', 'terminal'] })
  await $.session.start(DESKTOP_START)
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'terminal', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ key: 'engine-band' })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: '46%' })).toBeUndefined()
  await ui.unmount()
})

test('script が失敗したら、黙らずに理由の 1 行を出す', async ($, on) => {
  engineBeneath(on, [], { ...DESKTOP, exitCode: 1 })
  await $.session.start(DESKTOP_START)
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'desktop', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ type: 'Text', text: /ステータスバーを作れない: .*rc=1 boom/ })).toBeDefined()
  await ui.unmount()
})

test('script の出力が空 (rc=0) でも、黙って何も描かず、理由の 1 行を出す', async ($, on) => {
  engineBeneath(on, [], { ...DESKTOP, stdout: '' })
  await $.session.start(DESKTOP_START)
  // @ts-ignore: 上と同じ
  const ui = await $.ui.mount({ plugin: 'desktop-statusline', surface: 'desktop', component: 'AbovePrompt', props: PROPS })
  expect(await ui.find({ type: 'Text', text: /ステータスバーを作れない: .*出力が空/ })).toBeDefined()
  await ui.unmount()
})

test('60 秒ごとに作り直す (CLI の refreshInterval と同じ)', async ($, on) => {
  const runs: Run[] = []
  const clock = engineBeneath(on, runs, DESKTOP)
  await $.session.start(DESKTOP_START)
  await clock.advance(60_000)
  await clock.advance(60_000)
  expect(runs.length).toBe(3)
})

test('メインのターンの終わりにも、名簿が空のままでも作り直す。サブエージェントのターンでは作り直さない', async ($, on) => {
  const runs: Run[] = []
  const clock = engineBeneath(on, runs, DESKTOP)
  await $.session.start(DESKTOP_START)
  // @ts-ignore: 上と同じ
  await $.turn.complete(TURN)
  await clock.advance(1) // 裏で走らせた作り直しが落ち着くまで待つ
  expect(runs.length).toBe(2)
  // @ts-ignore: 上と同じ
  await $.turn.complete({ ...TURN, agentId: 'a1' })
  await clock.advance(1)
  expect(runs.length).toBe(2)
})

test('desktop が起こしたセッションにだけ /statusline-refresh を登録する (CLI には出さない)', async ($, on) => {
  const desktop: { name: string; immediate?: boolean }[] = []
  engineBeneath(on, [], { ...DESKTOP, commands: desktop })
  await $.session.start(DESKTOP_START)
  expect(desktop).toEqual([{ name: 'statusline-refresh', immediate: true }])
})

test('CLI のセッションでは /statusline-refresh を登録しない', async ($, on) => {
  const cli: { name: string; immediate?: boolean }[] = []
  engineBeneath(on, [], { ...CLI, commands: cli })
  await $.session.start(CLI_START)
  expect(cli).toEqual([])
})

test('/statusline-refresh は script を今すぐ呼び直し、結果を出力に返す', async ($, on) => {
  const runs: Run[] = []
  engineBeneath(on, runs, DESKTOP)
  await $.session.start(DESKTOP_START)
  expect(runs.length).toBe(1)
  const r = await $.command.run({ command: 'statusline-refresh' })
  expect(runs.length).toBe(2)
  expect(r.text).toBe('ステータスバーを作り直した')
})

test('/statusline-refresh は script が失敗したら、その理由を出力に返す', async ($, on) => {
  engineBeneath(on, [], { ...DESKTOP, exitCode: 1 })
  await $.session.start(DESKTOP_START)
  const r = await $.command.run({ command: 'statusline-refresh' })
  expect(r.text).toMatch(/ステータスバーを作れない: .*rc=1 boom/)
})
