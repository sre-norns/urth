import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

export default createKeyedFetchReducer({
  fetching: ActionType.WORKER_FETCHING,
  fetched: ActionType.WORKER_FETCHED,
  failed: ActionType.WORKER_FETCH_FAILED,
})
