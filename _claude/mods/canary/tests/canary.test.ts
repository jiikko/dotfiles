import type { On } from 'claude-code'
import { test, expect, mock } from 'claude-code/testing'

const START = { cwd: '/w', surface: 'terminal', isInteractive: true } as const

// test の on は engine の役 (plugin の下に座る)。session.start の engine は cwd を返すだけ
const engineBeneath = (on: On, writes: string[]) => {
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('fs.write', ($, e) => {
    writes.push(e.path)
  })
}

test('DOTFILES_MOD_CANARY_DIR があれば印を書く', async ($, on) => {
  const writes: string[] = []
  mock.env(on, { DOTFILES_MOD_CANARY_DIR: '/marks' })
  engineBeneath(on, writes)
  await $.session.start(START)
  expect(writes).toEqual(['/marks/loaded'])
})

test('DOTFILES_MOD_CANARY_DIR が無ければ何も書かない', async ($, on) => {
  const writes: string[] = []
  mock.env(on, {})
  engineBeneath(on, writes)
  await $.session.start(START)
  expect(writes).toEqual([])
})
