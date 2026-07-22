import {applyMiddleware, compose, legacy_createStore as createStore} from 'redux'
import {thunk} from 'redux-thunk'
import reducers from './reducers/index.js'

// The Redux DevTools bridge is read straight off the window rather than through
// the `redux-devtools-extension` package, which is deprecated in favour of
// @redux-devtools/extension and pins redux to v4 -- blocking this upgrade for
// the sake of a wrapper around one global.
const composeEnhancers = (typeof window !== 'undefined' && window.__REDUX_DEVTOOLS_EXTENSION_COMPOSE__) || compose

// redux-logger is dropped with it: last released in 2017, and it duplicates what
// DevTools already shows while filling the console during a run.
const store = createStore(reducers, {}, composeEnhancers(applyMiddleware(thunk)))

export default store
