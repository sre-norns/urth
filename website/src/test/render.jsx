import React from 'react'
import {render} from '@testing-library/react'
import {Provider} from 'react-redux'
import {ThemeProvider} from '@emotion/react'
import {applyMiddleware, legacy_createStore as createStore} from 'redux'
import {thunk} from 'redux-thunk'
import {Router} from 'wouter'
import {memoryLocation} from 'wouter/memory-location'
import reducers from '../reducers/index.js'
import {createTheme} from '../theme/index.js'

// Builds a store with the real reducers, so a test exercises the same state
// shape the app does rather than a hand-written stand-in.
export const createTestStore = (preloadedState = {}) => createStore(reducers, preloadedState, applyMiddleware(thunk))

// renderWithProviders mounts a component inside the providers the app relies on:
// redux, the emotion theme, and a router backed by in-memory history.
export const renderWithProviders = (
  ui,
  {preloadedState = {}, store = createTestStore(preloadedState), path = '/'} = {}
) => {
  const {hook} = memoryLocation({path, static: false, record: true})

  const Wrapper = ({children}) => (
    <Provider store={store}>
      <ThemeProvider theme={createTheme(false)}>
        <Router hook={hook}>{children}</Router>
      </ThemeProvider>
    </Provider>
  )

  return {store, ...render(ui, {wrapper: Wrapper})}
}
