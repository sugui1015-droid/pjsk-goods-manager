import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const styleSource = readFileSync(resolve(frontendRoot, 'src/style.css'), 'utf8')

function ruleBody(selector) {
  // Return the body of the first `selector { ... }` rule (selector matched
  // exactly at a line start), tolerant of multi-selector rules.
  const re = new RegExp(`(^|[},])\\s*${selector.replace(/[.*+?^${}()|[\\]\\\\]/g, '\\$&')}\\s*\\{([^}]*)\\}`, 'm')
  const m = styleSource.match(re)
  assert.ok(m, `missing CSS rule for ${selector}`)
  return m[2]
}

test('metric tiles are uniformly sized and centered', () => {
  const base = ruleBody('.metric-tile')
  assert.match(base, /min-height:\s*\d/)
  // A later rule centers content horizontally and vertically-distributed labels.
  assert.match(styleSource, /\.metric-tile\s*\{[^}]*align-items:\s*center[^}]*\}/)
  assert.match(styleSource, /\.metric-tile\s*\{[^}]*text-align:\s*center[^}]*\}/)
  // Long numbers wrap instead of overflowing the box.
  assert.match(styleSource, /\.metric-tile strong\s*\{[^}]*overflow-wrap:\s*anywhere/)
})

test('file inputs cannot force horizontal page scroll at 320px', () => {
  // The shared file input rule constrains the bare <input type=file>.
  assert.match(styleSource, /\.login-form input,\s*\.file-picker input\s*\{[\s\S]*?min-width:\s*0[\s\S]*?\}/)
})

test('equal-sized cards use min-height for stable heights', () => {
  assert.match(ruleBody('.entry-choice'), /min-height:\s*\d/)
  assert.match(ruleBody('.qr-card__preview'), /min-height:\s*\d/)
})

test('method buttons are equal width and height', () => {
  const btn = ruleBody('.query-method-button')
  assert.match(btn, /flex:\s*1/)
  assert.match(btn, /min-height:\s*\d/)
})
