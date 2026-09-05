import { readFileSync } from 'node:fs'

/**
 * Loads the real stylesheet out of app.html into the test document.
 *
 * Some card guarantees are CSS invariants, not markup — the badge overflow rule
 * is the actual fix for the title-overflow bug that helped kill the per-child
 * cards attempt. Asserting it means the browser's cascade has to be present, so
 * the page's own <style> block is the source rather than a copy that could
 * drift from it.
 */
const APP_HTML = 'worklog/internal/serve/static/app.html'

let css = null

export function appCss() {
  if (css === null) {
    const html = readFileSync(APP_HTML, 'utf8')
    const m = /<style>([\s\S]*?)<\/style>/.exec(html)
    if (!m) throw new Error(`no <style> block found in ${APP_HTML}`)
    css = m[1]
  }
  return css
}

export function withAppStyles(doc = document) {
  const el = doc.createElement('style')
  el.textContent = appCss()
  doc.head.appendChild(el)
  return () => el.remove()
}
