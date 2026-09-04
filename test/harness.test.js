import { render, screen } from '@testing-library/preact'
import { h } from 'preact'
import { expect, test } from 'vitest'

/**
 * Guards the setup file, not the app.
 *
 * @testing-library/preact 3.x does not auto-clean between tests, so without a
 * global afterEach(cleanup) the first render below would still be mounted for
 * the second, and getByTestId would throw "found multiple elements". These two
 * tests fail together the moment test/setup.js stops being applied — which is
 * how a silent config regression stays loud.
 */

const Marker = () => h('span', { 'data-testid': 'marker' }, 'x')

test('first render mounts a marker', () => {
  render(h(Marker, {}))
  expect(screen.getByTestId('marker')).toBeTruthy()
})

test('the previous test left nothing behind', () => {
  render(h(Marker, {}))
  expect(() => screen.getByTestId('marker')).not.toThrow()
  expect(document.querySelectorAll('[data-testid="marker"]')).toHaveLength(1)
})

test('the route is reset between tests too', () => {
  expect(location.hash).toBe('')
})
