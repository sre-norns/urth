import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

export default createKeyedFetchReducer({
  fetching: ActionType.WORKER_RESULTS_FETCHING,
  fetched: ActionType.WORKER_RESULTS_FETCHED,
  failed: ActionType.WORKER_RESULTS_FETCH_FAILED,
})
