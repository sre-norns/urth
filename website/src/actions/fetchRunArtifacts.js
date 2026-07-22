import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'
import {LabelArtifact} from '../utils/labels.js'

// Artifacts are not nested under a run in the API; they are their own resource,
// tied back to a run by label. Selecting on urth/result.name is how a run's
// output is found.
const fetchRunArtifacts = (runId) => async (dispatch) => {
  dispatch({type: ActionType.RUN_ARTIFACTS_FETCHING, key: runId})

  try {
    const selector = encodeURIComponent(`${LabelArtifact.ResultName}=${runId}`)
    const response = await apiGet(`/api/v1/artifacts?labels=${selector}`)

    dispatch({type: ActionType.RUN_ARTIFACTS_FETCHED, key: runId, response})
  } catch (error) {
    dispatch({type: ActionType.RUN_ARTIFACTS_FETCH_FAILED, key: runId, error})
  }
}

export default fetchRunArtifacts
