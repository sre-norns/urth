import React, {Fragment, useMemo} from 'react'
import {useMediaQuery} from '@react-hook/media-query'
import {ThemeProvider} from '@emotion/react'
import {Navigate, Route, Routes} from 'react-router-dom'
import {createTheme} from './theme/index.js'
import HeaderMock from './containers/HeaderMock.js'
import Scenarios from './pages/Scenarios.js'

export default () => {
  const dark = useMediaQuery('(prefers-color-scheme: dark)')
  const theme = useMemo(() => createTheme(dark), [dark])

  return (
    <ThemeProvider theme={theme}>
      <Fragment>
        <HeaderMock />
        <Routes>
          <Route path="/">
            <Route index element={<Navigate to="/scenarios"/>}/>
            <Route path="scenarios" element={<Scenarios/>}/>
            <Route path="*" element={<p>Unknown</p>}/>
          </Route>
        </Routes>
      </Fragment>
    </ThemeProvider>
  );
}