import {combineReducers} from 'redux'
import scenarios from './scenarios.js'
import scenarioActions from './scenarioActions.js'
import scenario from './scenario.js'


export default combineReducers({
  scenarios,
  scenarioActions,
  scenario,
})
