import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'
import {LabelWorker} from '../utils/labels.js'

export const WORKER_RUN_LIMIT = 10

// Results carry the physical executor as a server-derived label. Querying that
// label keeps the page on the normal cross-scenario results API and lets the
// response's `total` report the complete execution count while `pageSize`
// bounds the history rendered on screen.
const fetchWorkerResults = (workerName) => async (dispatch) => {
  dispatch({type: ActionType.WORKER_RESULTS_FETCHING, key: workerName})

  try {
    const query = new URLSearchParams({
      labels: `${LabelWorker.Name} = ${workerName}`,
      pageSize: String(WORKER_RUN_LIMIT),
    })
    const response = await apiGet(`/api/v1/results?${query}`)

    dispatch({type: ActionType.WORKER_RESULTS_FETCHED, key: workerName, response})
  } catch (error) {
    dispatch({type: ActionType.WORKER_RESULTS_FETCH_FAILED, key: workerName, error})
  }
}

export default fetchWorkerResults
