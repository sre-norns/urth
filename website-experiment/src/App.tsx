import {Navigate, Route, Routes, useParams} from 'react-router-dom'
import {Layout} from './components/Layout'
import {ArtifactDetailPage} from './pages/ArtifactDetail'
import {ArtifactsPage} from './pages/Artifacts'
import {RunDetailPage} from './pages/RunDetail'
import {RunnerDetailPage} from './pages/RunnerDetail'
import {RunnersPage} from './pages/Runners'
import {ScenarioDetailPage} from './pages/ScenarioDetail'
import {ScenarioFormPage} from './pages/ScenarioForm'
import {ScenarioRunsPage} from './pages/ScenarioRuns'
import {ScenariosPage} from './pages/Scenarios'
import {RunsPage} from './pages/Runs'
import {WorkerDetailPage} from './pages/WorkerDetail'
import {WorkersPage} from './pages/Workers'
import {Empty, LinkButton, PageHeader} from './components/ui'

function LegacyResultRedirect() {
  const {runName = ''} = useParams()
  return <Navigate to={`/runs/${encodeURIComponent(decodeURIComponent(runName))}`} replace />
}

function NotFound() {
  return <div className="page page-narrow"><PageHeader title="Page not found" description="The resource may have moved or no longer exists." /><Empty title="Nothing at this address" action={<LinkButton to="/scenarios">Return to scenarios</LinkButton>} /></div>
}

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Navigate to="/scenarios" replace />} />
        <Route path="scenarios" element={<ScenariosPage />} />
        <Route path="scenarios/new" element={<ScenarioFormPage />} />
        <Route path="scenarios/:scenarioName" element={<ScenarioDetailPage />} />
        <Route path="scenarios/:scenarioName/edit" element={<ScenarioFormPage />} />
        <Route path="scenarios/:scenarioName/runs" element={<ScenarioRunsPage />} />
        <Route path="scenarios/:scenarioName/runs/:runName" element={<RunDetailPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="runs/:runName" element={<RunDetailPage />} />
        <Route path="results" element={<Navigate to="/runs" replace />} />
        <Route path="results/:runName" element={<LegacyResultRedirect />} />
        <Route path="runners" element={<RunnersPage />} />
        <Route path="runners/:runnerName" element={<RunnerDetailPage />} />
        <Route path="workers" element={<WorkersPage />} />
        <Route path="workers/:workerName" element={<WorkerDetailPage />} />
        <Route path="artifacts" element={<ArtifactsPage />} />
        <Route path="artifacts/:artifactName" element={<ArtifactDetailPage />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
