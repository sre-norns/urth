import ActionType from './ActionType.js'
import { apiGet } from '../utils/api.js'

// Runs are fetched by name alone, through the cross-scenario endpoint. Run names
// are generated to be unique, so a run can be opened directly from the Results
// list without knowing which scenario produced it -- and the same page then
// serves both /results/:runId and /scenarios/:id/runs/:runId.
const fetchRun = (runId) => async (dispatch) => {
  dispatch({ type: ActionType.RUN_FETCHING, key: runId })

  try {
    const response = await apiGet(`/api/v1/results/${runId}`)

    dispatch({ type: ActionType.RUN_FETCHED, key: runId, response })
  } catch (error) {
    dispatch({ type: ActionType.RUN_FETCH_FAILED, key: runId, error })
  }
}

export default fetchRun
