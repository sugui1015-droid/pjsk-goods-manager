export type PaymentNoticeSubmission = {
  status: string
  reviewed_at?: string
}

export type PaymentNoticeState = {
  hasNotice: boolean
  text: string
}

const reviewedStatuses = new Set(['approved', 'rejected'])

function parsedTime(value?: string) {
  if (!value) return 0
  const time = Date.parse(value)
  return Number.isFinite(time) ? time : 0
}

function paymentViewedStorageKey(cn?: string) {
  const normalizedCN = cn?.trim().toUpperCase()
  return normalizedCN ? `pjsk:lastPaymentViewedAt:${normalizedCN}` : ''
}

export function readPaymentViewedAt(cn?: string) {
  const key = paymentViewedStorageKey(cn)
  if (!key) return ''
  try {
    return window.localStorage.getItem(key) ?? ''
  } catch {
    // Storage may be unavailable in private/restricted browser contexts. The
    // reminder can safely reappear because this timestamp is UI state only.
    return ''
  }
}

export function writePaymentViewedAt(cn: string | undefined, viewedAt: string) {
  const key = paymentViewedStorageKey(cn)
  if (!key) return
  try {
    window.localStorage.setItem(key, viewedAt)
  } catch {
    // The caller still keeps the in-memory timestamp for the current session.
  }
}

// Pure UI-state decision: payment submissions remain the business source of
// truth, while lastViewedTime only remembers what this browser has displayed.
export function getPaymentNoticeState(
  submissions: PaymentNoticeSubmission[],
  lastViewedTime: string,
): PaymentNoticeState {
  const viewedAt = parsedTime(lastViewedTime)
  const hasNotice = submissions.some((submission) => (
    reviewedStatuses.has(submission.status)
    && parsedTime(submission.reviewed_at) > viewedAt
  ))

  return {
    hasNotice,
    text: hasNotice ? '付款审核有更新' : '',
  }
}
