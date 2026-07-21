import ActionType from './ActionType.js'
import { apiGet } from '../utils/api.js'
import { LabelRunner } from '../utils/labels.js'

// Workers are their own resource, tied to a runner by label rather than nested
// under it, so a runner's workers are found by selector.
const fetchWorkers = (runnerName) => async (dispatch) => {
  const key = runnerName || 'all'
  dispatch({ type: ActionType.WORKERS_FETCHING, key })

  try {
    const query = runnerName
      ? `?labels=${encodeURIComponent(`${LabelRunner.Name}=${runnerName}`)}`
      : ''
    const response = await apiGet(`/api/v1/workers${query}`)

    dispatch({ type: ActionType.WORKERS_FETCHED, key, response })
  } catch (error) {
    dispatch({ type: ActionType.WORKERS_FETCH_FAILED, key, error })
  }
}

export default fetchWorkers
