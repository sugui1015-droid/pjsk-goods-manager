import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import { localDevelopmentFrontendOrigins } from '../src/developmentOrigins.ts'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')

test('development mode retains the two exact local Vite origins', () => {
  assert.deepEqual(localDevelopmentFrontendOrigins(), [
    'http://localhost:5173',
    'http://127.0.0.1:5173',
  ])
})

test('fallback origins are gated by Vite DEV and production falls back to same-origin', () => {
  assert.match(
    appSource,
    /frontendOrigins:\s*import\.meta\.env\.DEV\s*\?\s*localDevelopmentFrontendOrigins\(\)\s*:\s*\[\]/,
  )
  assert.equal(appSource.includes("frontendOrigins: ['http://localhost:5173'"), false)
})
