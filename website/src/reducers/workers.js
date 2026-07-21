import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

// Keyed by runner name, so one runner's worker list does not replace another's.
export default createKeyedFetchReducer({
  fetching: ActionType.WORKERS_FETCHING,
  fetched: ActionType.WORKERS_FETCHED,
  failed: ActionType.WORKERS_FETCH_FAILED,
})
