import React, { Fragment, useMemo } from 'react'
import { useMediaQuery } from '@react-hook/media-query'
import { ThemeProvider } from '@emotion/react'
import { createTheme } from './theme/index.js'
import Header from './containers/Header.jsx'
import Scenarios from './pages/Scenarios.jsx'
import ScenarioDetail from './pages/ScenarioDetail.jsx'
import ScenarioViewer from './pages/ScenarioViewer.jsx'
import RunDetail from './pages/RunDetail.jsx'
import Results from './pages/Results.jsx'
import Runners from './pages/Runners.jsx'
import RunnerDetail from './pages/RunnerDetail.jsx'

import { Redirect, Route, Switch } from 'wouter'

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
          {/* `new` is the create form, so it has to be matched before the
              detail route would treat it as a scenario name. */}
          <Route path="/scenarios/new/edit">{() => <ScenarioViewer scenarioId="new" edit />}</Route>
          <Route path="/scenarios/:scenarioId/edit">
            {(params) => <ScenarioViewer scenarioId={params.scenarioId} edit />}
          </Route>
          <Route path="/scenarios/:scenarioId/runs/:runId">
            {(params) => <RunDetail scenarioId={params.scenarioId} runId={params.runId} />}
          </Route>
          <Route path="/scenarios/:scenarioId">{(params) => <ScenarioDetail scenarioId={params.scenarioId} />}</Route>
          <Route path="/results">{() => <Results />}</Route>
          {/* A run can be opened without knowing its scenario; the page reads
              the scenario from the run itself. */}
          <Route path="/results/:runId">{(params) => <RunDetail runId={params.runId} />}</Route>
          <Route path="/runners">{() => <Runners />}</Route>
          <Route path="/runners/:runnerId">{(params) => <RunnerDetail runnerId={params.runnerId} />}</Route>
          <Route>{() => <p>Not found</p>}</Route>
        </Switch>
      </>
    </ThemeProvider>
  )
}
