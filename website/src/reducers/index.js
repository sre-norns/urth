import { combineReducers } from 'redux'
import scenarios from './scenarios.js'
import scenarioActions from './scenarioActions.js'
import scenario from './scenario.js'
import scenarioResults from './scenarioResults.js'
import run from './run.js'
import runArtifacts from './runArtifacts.js'
import artifactContent from './artifactContent.js'

export default combineReducers({
  scenarios,
  scenarioActions,
  scenario,
  scenarioResults,
  run,
  runArtifacts,
  artifactContent,
})
