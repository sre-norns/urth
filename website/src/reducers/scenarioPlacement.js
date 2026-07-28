import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

// Keyed by scenario name: the placement preview is per scenario, and a page
// switching between two of them must not show the first one's answer.
export default createKeyedFetchReducer({
  fetching: ActionType.SCENARIO_PLACEMENT_FETCHING,
  fetched: ActionType.SCENARIO_PLACEMENT_FETCHED,
  failed: ActionType.SCENARIO_PLACEMENT_FETCH_FAILED,
})
