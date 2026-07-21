import { combineReducers } from 'redux'
import scenarios from './scenarios.js'
import scenarioActions from './scenarioActions.js'
import scenario from './scenario.js'
import scenarioResults from './scenarioResults.js'
import run from './run.js'
import runArtifacts from './runArtifacts.js'
import artifactContent from './artifactContent.js'
import runners from './runners.js'
import runner from './runner.js'
import workers from './workers.js'

export default combineReducers({
  scenarios,
  scenarioActions,
  scenario,
  scenarioResults,
  run,
  runArtifacts,
  artifactContent,
  runners,
  runner,
  workers,
})
