import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import {
  DEFAULT_UPLOAD_TIMEOUT_MS,
  UploadCanceledError,
  UploadHttpError,
  UploadTimeoutError,
  uploadFormWithProgress,
} from '../src/api/upload.ts'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')
const clientSource = readFileSync(resolve(frontendRoot, 'src/api/client.ts'), 'utf8')

// 假 XHR：让上传状态机（进度、阶段、超时、取消）可以在 Node 里完整驱动，
// 不需要浏览器。生产上正是这段逻辑缺失，导致 10.3 秒无反馈、188 秒被中断。
class FakeXHR {
  constructor() {
    this.upload = { onprogress: null }
    this.onload = null
    this.onerror = null
    this.ontimeout = null
    this.onabort = null
    this.withCredentials = false
    this.timeout = 0
    this.status = 0
    this.responseText = ''
    this.aborted = false
    this.sent = null
    this.openedWith = null
  }

  open(method, url) { this.openedWith = { method, url } }
  send(body) { this.sent = body }
  abort() {
    this.aborted = true
    this.onabort?.()
  }

  emitProgress(loaded, total) {
    this.upload.onprogress?.({ lengthComputable: true, loaded, total })
  }

  respond(status, payload) {
    this.status = status
    this.responseText = typeof payload === 'string' ? payload : JSON.stringify(payload)
    this.onload?.()
  }
}

function startUpload(overrides = {}) {
  const xhr = new FakeXHR()
  const phases = []
  const percents = []
  const handle = uploadFormWithProgress({
    url: '/api/query/payment-submissions',
    form: new FormData(),
    onProgress: (percent) => percents.push(percent),
    onPhase: (phase) => phases.push(phase),
    xhrFactory: () => xhr,
    ...overrides,
  })
  return { xhr, handle, phases, percents }
}

test('上传过程按 0→N%→等待确认→成功推进', async () => {
  const { xhr, handle, phases, percents } = startUpload()
  xhr.emitProgress(25, 100)
  xhr.emitProgress(60, 100)
  xhr.emitProgress(100, 100)
  xhr.respond(201, { submission: { id: 'x', payable_amount: 12 } })

  const response = await handle.promise
  assert.equal(response.submission.id, 'x')
  assert.deepEqual(percents, [0, 25, 60, 100])
  assert.deepEqual(phases, ['uploading', 'uploading', 'uploading', 'awaiting', 'success'])
})

test('最后一个字节发出后进入「等待服务器确认」，而不是停在 100% 假装完成', () => {
  const { xhr, phases } = startUpload()
  xhr.emitProgress(100, 100)
  assert.equal(phases.at(-1), 'awaiting')
})

test('只有服务器返回 2xx 才算成功', async () => {
  const { xhr, handle, phases } = startUpload()
  xhr.emitProgress(100, 100)
  xhr.respond(500, { error: '服务器内部错误' })
  await assert.rejects(handle.promise, (error) => error instanceof UploadHttpError && error.status === 500)
  assert.equal(phases.includes('success'), false, '服务器失败时不得出现 success 阶段')
})

test('超时被识别为可重试的超时错误，并带上不要重复提交的提示', async () => {
  const { xhr, handle } = startUpload()
  xhr.ontimeout()
  await assert.rejects(handle.promise, (error) => {
    assert.ok(error instanceof UploadTimeoutError)
    assert.match(error.message, /上传超时/)
    assert.match(error.message, /请勿重复提交/)
    return true
  })
})

test('默认超时为 120 秒并真正写进 XHR', () => {
  const { xhr } = startUpload()
  assert.equal(DEFAULT_UPLOAD_TIMEOUT_MS, 120_000)
  assert.equal(xhr.timeout, 120_000)
})

test('用户取消会中止请求，且绝不显示成功', async () => {
  const { xhr, handle, phases } = startUpload()
  xhr.emitProgress(40, 100)
  handle.cancel()
  assert.equal(xhr.aborted, true)
  await assert.rejects(handle.promise, (error) => error instanceof UploadCanceledError)
  assert.equal(phases.includes('success'), false)
  assert.equal(phases.at(-1), 'canceled')
})

test('取消已完成的上传不会把已成功的结果改写成失败', async () => {
  const { xhr, handle } = startUpload()
  xhr.respond(201, { submission: { id: 'done' } })
  handle.cancel()
  const response = await handle.promise
  assert.equal(response.submission.id, 'done')
  assert.equal(xhr.aborted, false, '已结束的请求不应再被 abort')
})

test('网络错误后 promise 结束，界面可重试', async () => {
  const { xhr, handle, phases } = startUpload()
  xhr.onerror()
  await assert.rejects(handle.promise, (error) => error instanceof Error)
  assert.equal(phases.at(-1), 'error')
})

test('上传携带凭证，走 POST', () => {
  const { xhr } = startUpload()
  assert.equal(xhr.withCredentials, true)
  assert.equal(xhr.openedWith.method, 'POST')
})

test('无法解析的错误响应回退为通用提示，不泄漏内部信息', async () => {
  const { xhr, handle } = startUpload()
  xhr.respond(500, '<html>nginx internal trace</html>')
  await assert.rejects(handle.promise, (error) => {
    assert.equal(error.message, '上传失败，请重试。')
    return true
  })
})

// ---- App.vue 接线 ----

const submitBlock = (() => {
  const start = appSource.indexOf('async function submitUserProof()')
  assert.ok(start > 0, '找不到 submitUserProof')
  const end = appSource.indexOf('function submissionStatusLabel', start)
  assert.ok(end > start)
  return appSource.slice(start, end)
})()

test('付款凭证上传走 XHR 通道，不再走无法上报进度的 fetch', () => {
  assert.match(submitBlock, /uploadFormWithProgress</)
  assert.equal(submitBlock.includes('submitPaymentSubmission('), false)
  assert.equal(
    clientSource.includes('export function submitPaymentSubmission'),
    false,
    '基于 fetch 的旧上传函数应删除，否则会有人再用回去',
  )
})

test('同一页面只允许一个上传在飞行中', () => {
  assert.match(
    submitBlock,
    /if \(!file \|\| submissionUploading\.value \|\| submissionUploadHandle\) return/,
    '必须同时挡住重复点击和并发句柄',
  )
})

test('上传期间禁用提交按钮与文件选择', () => {
  assert.match(appSource, /query-submission-button[^>]*:disabled="!canSubmitProof/)
  assert.match(appSource, /submission-file-input[^>]*:disabled="submissionUploading"/)
})

test('提供取消上传按钮', () => {
  assert.match(appSource, /query-submission-cancel[\s\S]{0,160}cancelSubmissionUpload\(true\)/)
})

test('重新选择文件与离开页面都会中止旧请求', () => {
  const fileChange = appSource.slice(
    appSource.indexOf('async function onSubmissionFileChange'),
    appSource.indexOf('async function submitUserProof'),
  )
  assert.match(fileChange, /cancelSubmissionUpload\(false\)/)
  const unmount = appSource.slice(appSource.indexOf('onUnmounted(() => {'))
  assert.match(unmount, /cancelSubmissionUpload\(false\)/)
})

test('无论成功、超时、取消还是网络错误，按钮状态都会恢复', () => {
  assert.match(submitBlock, /finally \{[\s\S]*submissionUploading\.value = false/)
})

test('每次选择文件生成一次性幂等键，并随请求提交', () => {
  const fileChange = appSource.slice(
    appSource.indexOf('async function onSubmissionFileChange'),
    appSource.indexOf('async function submitUserProof'),
  )
  assert.match(fileChange, /submissionRequestID\.value = newIdempotencyKey\(\)/)
  assert.match(submitBlock, /form\.append\('request_id', submissionRequestID\.value\)/)
  assert.equal(
    submitBlock.includes('newIdempotencyKey()'),
    false,
    '提交时不得重新生成键，否则重试会被当成新提交',
  )
})

test('页面展示压缩前后大小与上传阶段', () => {
  assert.match(appSource, /submissionCompressionNote/)
  assert.match(appSource, /正在处理图片…/)
  assert.match(appSource, /正在上传 \$\{submissionProgress\.value\}%/)
  assert.match(appSource, /正在等待服务器确认…/)
})
