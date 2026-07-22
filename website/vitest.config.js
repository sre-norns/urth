import {defineConfig} from 'vitest/config'
import {transformAsync} from '@babel/core'

// @vitejs/plugin-react v6 transforms JSX with oxc and exposes no babel hook, so
// two things this app relies on cannot be expressed through it:
//
//   - JSX lives in .js files, which oxc refuses to parse as JSX,
//   - @emotion/babel-plugin must run, or emotion's component selectors
//     (`${NameSpan} { ... }` inside a styled template) throw at render time.
//
// Running the project's own .babelrc is both simpler and closer to the truth:
// it is exactly what babel-loader does in the webpack build, so the tests and
// the shipped bundle go through the same transforms.
const babelTransform = () => ({
  name: 'urth:babel',
  enforce: 'pre',
  async transform(code, id) {
    if (!/\/src\/.*\.jsx?$/.test(id) || id.includes('node_modules')) {
      return null
    }

    const result = await transformAsync(code, {
      filename: id,
      babelrc: false,
      configFile: false,
      sourceMaps: true,
      // Mirrors .babelrc, with two adjustments for the test environment:
      // modules are left as ESM (vitest cannot load CommonJS test files) and
      // preset-env targets the running node rather than browsers.
      presets: [
        ['@babel/preset-env', {targets: {node: 'current'}, modules: false}],
        ['@babel/preset-react', {runtime: 'automatic', importSource: '@emotion/react'}],
      ],
      plugins: ['@emotion/babel-plugin'],
    })

    return result ? {code: result.code, map: result.map} : null
  },
})

export default defineConfig({
  plugins: [babelTransform()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.js'],
    include: ['src/**/*.test.{js,jsx}'],
  },
})
