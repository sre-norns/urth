import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

export default createKeyedFetchReducer({
  fetching: ActionType.RUNNER_FETCHING,
  fetched: ActionType.RUNNER_FETCHED,
  failed: ActionType.RUNNER_FETCH_FAILED,
})
