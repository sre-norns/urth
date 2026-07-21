import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

// Keyed by `${scenarioId}/${runId}`: run names are unique per scenario, not
// globally.
export default createKeyedFetchReducer({
  fetching: ActionType.RUN_FETCHING,
  fetched: ActionType.RUN_FETCHED,
  failed: ActionType.RUN_FETCH_FAILED,
})
