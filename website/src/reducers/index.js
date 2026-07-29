import {combineReducers} from 'redux'
import scenarios from './scenarios.js'
import scenarioActions from './scenarioActions.js'
import scenario from './scenario.js'
import scenarioResults from './scenarioResults.js'
import scenarioPlacement from './scenarioPlacement.js'
import run from './run.js'
import runArtifacts from './runArtifacts.js'
import artifactContent from './artifactContent.js'
import runners from './runners.js'
import runner from './runner.js'
import workers from './workers.js'
import worker from './workerDetail.js'
import workerResults from './workerResults.js'
import results from './results.js'
import probKinds from './probKinds.js'
import deadLetters from './deadLetters.js'

export default combineReducers({
  scenarios,
  scenarioActions,
  scenario,
  scenarioResults,
  scenarioPlacement,
  run,
  runArtifacts,
  artifactContent,
  runners,
  runner,
  workers,
  worker,
  workerResults,
  results,
  probKinds,
  deadLetters,
})
