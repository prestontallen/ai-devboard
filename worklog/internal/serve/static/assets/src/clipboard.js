/**
 * Copy support, and href sanitising.
 *
 * The board binds 0.0.0.0 and is used over a LAN IP, which is a non-secure
 * context — `navigator.clipboard` is undefined there. The execCommand path is
 * the one that actually works on the deployment this runs on, so it is carried
 * over from index.html rather than modernised away.
 *
 * happy-dom implements no `document.execCommand`, so that branch cannot be
 * exercised by a render test; the tested path is `navigator.clipboard` only.
 * Documented in the contract rather than papered over.
 */

export function fallbackCopy(text, doc = document) {
  const ta = doc.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  doc.body.appendChild(ta)
  ta.select()
  let ok = false
  try {
    ok = typeof doc.execCommand === 'function' && doc.execCommand('copy')
  } catch {
    ok = false
  }
  doc.body.removeChild(ta)
  return ok
}

export async function copyText(text, nav = typeof navigator === 'undefined' ? undefined : navigator) {
  if (nav && nav.clipboard && typeof nav.clipboard.writeText === 'function') {
    try {
      await nav.clipboard.writeText(text)
      return true
    } catch {
      // Permission denied or a non-secure context that still exposes the API.
    }
  }
  return fallbackCopy(text)
}

/**
 * Only http(s), mailto, or scheme-less URLs get a live href. A task file is
 * hand-editable, so a `javascript:` value reaching an href is a real path —
 * the old UI shipped that bug once.
 */
export function safeHref(url) {
  const s = String(url ?? '').trim()
  if (!s) return null
  if (/^(https?:|mailto:)/i.test(s)) return s
  if (/^[a-z][a-z0-9+.-]*:/i.test(s)) return null // any other scheme
  return s
}
