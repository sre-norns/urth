import ActionType from '../actions/ActionType.js'
import createKeyedFetchReducer from './keyedFetch.js'

export default createKeyedFetchReducer({
  fetching: ActionType.ARTIFACT_CONTENT_FETCHING,
  fetched: ActionType.ARTIFACT_CONTENT_FETCHED,
  failed: ActionType.ARTIFACT_CONTENT_FETCH_FAILED,
})
