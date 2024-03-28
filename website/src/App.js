import React, {Fragment, useMemo} from 'react'
import {useMediaQuery} from '@react-hook/media-query'
import {ThemeProvider} from '@emotion/react'
import {createTheme} from './theme/index.js'
import Header from './containers/Header.js'
import Scenarios from './pages/Scenarios.js'
import ScenarioViewer from './pages/ScenarioViewer.js'
import {Redirect, Route, Switch} from 'wouter'

export default () => {
  const dark = useMediaQuery('(prefers-color-scheme: dark)')
  const theme = useMemo(() => createTheme(dark), [dark])

  return (
    <ThemeProvider theme={theme}>
      <>
        <Header />
        <Switch>
          <Route path="/">{() => <Redirect to="/scenarios" />}</Route>
          <Route path="/scenarios">{() => <Scenarios />}</Route>
          <Route path="/scenarios/:scenarioId">{(params) => <ScenarioViewer scenarioId={params.scenarioId} />}</Route>
          <Route path="/scenarios/:scenarioId/edit">
            {(params) => <ScenarioViewer scenarioId={params.scenarioId} edit />}
          </Route>
          <Route>{() => <p>Not found</p>}</Route>
        </Switch>
      </>
    </ThemeProvider>
  )
}
