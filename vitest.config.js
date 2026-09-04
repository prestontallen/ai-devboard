// A plain object, not defineConfig(), so vite stays a transitive detail of
// vitest rather than something this repo imports. There is no build step:
// the browser loads the same untranspiled ESM the tests import.
export default {
  test: {
    environment: 'happy-dom',
    include: ['test/**/*.test.js'],
    setupFiles: ['test/setup.js'],
  },
}
