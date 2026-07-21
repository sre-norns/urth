import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

export default createKeyedFetchReducer({
  fetching: ActionType.RUN_ARTIFACTS_FETCHING,
  fetched: ActionType.RUN_ARTIFACTS_FETCHED,
  failed: ActionType.RUN_ARTIFACTS_FETCH_FAILED,
})
