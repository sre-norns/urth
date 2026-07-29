import '@testing-library/jest-dom/vitest'
import {afterAll, afterEach, beforeAll} from 'vitest'
import {server} from './server'

beforeAll(() => {
  server.listen({onUnhandledRequest: 'error'})
  const interceptedFetch = globalThis.fetch
  globalThis.fetch = (input, init) => {
    if (typeof input === 'string' && input.startsWith('/')) {
      return interceptedFetch(new URL(input, 'http://localhost'), init)
    }
    return interceptedFetch(input, init)
  }
})
afterEach(() => server.resetHandlers())
afterAll(() => server.close())
