import { test, expect } from 'claude-code/testing'
import { parseCounts, segments } from '../hooks/band'

const HUMAN = 'overdue=1\nsoon=2\nhuman=5\nbroken=0\n'
const RETRO = 'retro=3\nheld=1\nodd=0\n'

test('--counts の出力を読む', () => {
  expect(parseCounts(HUMAN, RETRO)).toEqual({ overdue: 1, soon: 2, broken: 0, retro: 3, error: null })
})

test('どちらも何も出さない (issue dir が無い) なら null', () => {
  expect(parseCounts('', '\n')).toBe(null)
})

test('script の error= は error に入れる', () => {
  expect(parseCounts('error=lib を読めない\n', RETRO)?.error).toBe('lib を読めない')
})

test('案 3: 余裕のある human だけなら何も出さない', () => {
  expect(segments({ overdue: 0, soon: 0, broken: 0, retro: 0, error: null })).toEqual([])
})

test('案 3: 期限切れ・期限が近い・期限が読めない・retro を、この順で出す', () => {
  expect(segments({ overdue: 1, soon: 2, broken: 1, retro: 3, error: null }).map(s => s.text)).toEqual([
    '期限切れの human 1 件',
    '期限が近い human 2 件',
    '期限の読めない human 1 件',
    'retro 未決着 3',
  ])
})

test('数えられなかったことは黙らずに出す', () => {
  expect(segments({ overdue: 0, soon: 0, broken: 0, retro: 0, error: 'x' })[0]?.text).toBe('issue の催促を数えられない: x')
})
