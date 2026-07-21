// Upload transport for payment proofs.
//
// The shared `postForm` helper uses fetch(), which cannot report *upload*
// progress: the returned promise settles only once the response starts, so a
// user on a slow uplink sees a frozen page for the entire body transfer. That
// is exactly what production showed — 10.3 s of silence on a good run, and
// abandoned attempts at 24 s and 188 s with no feedback and no timeout.
//
// XMLHttpRequest is used here (and only here) because it exposes
// upload.onprogress. The XHR is injectable so the whole state machine —
// progress, phases, timeout, abort — is unit-testable in Node with no browser.

/**
 * Phases surfaced to the user. 'awaiting' is the gap after the last byte is
 * sent but before the server answers: without it the bar sits at 100% and
 * looks stuck, which is what makes people click submit a second time.
 */
export type UploadPhase = 'processing' | 'uploading' | 'awaiting' | 'success' | 'error' | 'canceled'

export class UploadTimeoutError extends Error {
  constructor() {
    super('上传超时，请检查网络后重试。系统未提示成功前，请勿重复提交。')
    this.name = 'UploadTimeoutError'
  }
}

export class UploadCanceledError extends Error {
  constructor() {
    super('已取消上传。')
    this.name = 'UploadCanceledError'
  }
}

export class UploadHttpError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'UploadHttpError'
    this.status = status
  }
}

/** The slice of XMLHttpRequest this module relies on, so tests can fake it. */
export type UploadXHR = {
  open(method: string, url: string): void
  send(body: FormData): void
  abort(): void
  withCredentials: boolean
  timeout: number
  status: number
  responseText: string
  upload: { onprogress: ((event: { lengthComputable: boolean; loaded: number; total: number }) => void) | null }
  onload: (() => void) | null
  onerror: (() => void) | null
  ontimeout: (() => void) | null
  onabort: (() => void) | null
}

export type UploadRequest = {
  url: string
  form: FormData
  timeoutMs?: number
  onProgress?: (percent: number) => void
  onPhase?: (phase: UploadPhase) => void
  xhrFactory?: () => UploadXHR
}

export type UploadHandle<T> = {
  promise: Promise<T>
  /** Aborts in flight; the promise rejects with UploadCanceledError. */
  cancel: () => void
}

/** 120 s: comfortably above a healthy compressed upload, short enough that a
 * dead connection surfaces as a retryable error instead of an endless spinner. */
export const DEFAULT_UPLOAD_TIMEOUT_MS = 120_000

export function uploadFormWithProgress<T>(request: UploadRequest): UploadHandle<T> {
  const {
    url,
    form,
    timeoutMs = DEFAULT_UPLOAD_TIMEOUT_MS,
    onProgress,
    onPhase,
    xhrFactory = () => new XMLHttpRequest() as unknown as UploadXHR,
  } = request

  const xhr = xhrFactory()
  let settled = false
  let canceled = false

  const promise = new Promise<T>((resolve, reject) => {
    const finish = (action: () => void) => {
      if (settled) return
      settled = true
      action()
    }

    xhr.upload.onprogress = (event) => {
      if (!event.lengthComputable || !(event.total > 0)) return
      const percent = Math.min(100, Math.round((event.loaded / event.total) * 100))
      onProgress?.(percent)
      onPhase?.(percent >= 100 ? 'awaiting' : 'uploading')
    }

    xhr.onload = () => {
      finish(() => {
        const status = xhr.status
        let payload: unknown = null
        try {
          payload = xhr.responseText ? JSON.parse(xhr.responseText) : null
        } catch {
          payload = null
        }
        if (status >= 200 && status < 300) {
          onPhase?.('success')
          resolve(payload as T)
          return
        }
        // Server-side wording is already user-safe; the generic fallback keeps
        // internal detail out of the UI when it is missing or unparsable.
        const message =
          (payload && typeof payload === 'object' && typeof (payload as { error?: unknown }).error === 'string'
            ? (payload as { error: string }).error
            : '') || '上传失败，请重试。'
        onPhase?.('error')
        reject(new UploadHttpError(status, message))
      })
    }

    xhr.onerror = () => {
      finish(() => {
        onPhase?.('error')
        reject(new Error('网络错误，上传未完成，请重试。'))
      })
    }

    xhr.ontimeout = () => {
      finish(() => {
        onPhase?.('error')
        reject(new UploadTimeoutError())
      })
    }

    xhr.onabort = () => {
      finish(() => {
        onPhase?.(canceled ? 'canceled' : 'error')
        reject(new UploadCanceledError())
      })
    }

    xhr.open('POST', url)
    xhr.withCredentials = true
    xhr.timeout = timeoutMs
    onPhase?.('uploading')
    onProgress?.(0)
    xhr.send(form)
  })

  return {
    promise,
    cancel: () => {
      if (settled) return
      canceled = true
      xhr.abort()
    },
  }
}
