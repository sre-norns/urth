import '@testing-library/jest-dom/vitest'
import {cleanup} from '@testing-library/react'
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
afterEach(() => { cleanup(); server.resetHandlers() })
afterAll(() => server.close())

// Node 25 ships a localStorage global of its own; started without
// --localstorage-file it has no working methods, and it shadows jsdom's. The
// shared shell keeps its sidebar state there, so give tests a working one.
if (typeof globalThis.localStorage?.getItem !== 'function') {
  const values = new Map<string, string>()
  const storage: Storage = {
    get length() {
      return values.size
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => void values.delete(key),
    setItem: (key, value) => void values.set(key, String(value)),
  }
  Object.defineProperty(globalThis, 'localStorage', {value: storage, configurable: true})
}

// jsdom has no modal dialogs. Enough of the API for the kit's Dialog to mount,
// as in the kit's own tests.
if (!HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.open = false
  }
}
