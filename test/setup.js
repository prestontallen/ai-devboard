import { cleanup } from '@testing-library/preact'
import { afterEach } from 'vitest'

// @testing-library/preact 3.x does not auto-clean between tests the way its
// React sibling does, so without this a second render leaves the first one's
// DOM in place and any `screen` query finds duplicate elements. chip.test.js
// only escaped it by scoping every query to its own container.
afterEach(cleanup)

// Each test starts on a clean route; the router reads location.hash at mount.
afterEach(() => {
  location.hash = ''
})
