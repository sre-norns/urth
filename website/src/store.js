import {applyMiddleware, legacy_createStore as createStore} from 'redux'
import thunk from 'redux-thunk'
import {createLogger} from 'redux-logger'
import {composeWithDevTools} from 'redux-devtools-extension'
import reducers from './reducers/index.js'

const middleware = [thunk, createLogger()]

const composedEnhancers = composeWithDevTools(applyMiddleware(...middleware))

const store = createStore(reducers, {}, composedEnhancers)

export default store
