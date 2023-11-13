import React from 'react'
import {createRoot} from 'react-dom/client'
import {BrowserRouter} from 'react-router-dom'
import {Provider} from 'react-redux'
import store from './store.js'

import 'the-new-css-reset/css/reset.css'
import '@fontsource-variable/roboto-flex'
import '@icon/foundation-icons/foundation-icons.css'
import './index.scss'
import App from './App.js'

const container = document.getElementById('app')
const root = createRoot(container)
root.render(
  <React.StrictMode>
    <Provider store={store}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </Provider>
  </React.StrictMode>
)
