import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

// A WorkerInstance is addressed by its current registration name. The server
// computes presence while serving this response, so polling this endpoint is
// also how a mounted detail page observes a worker going quiet.
const fetchWorker = (workerName) => async (dispatch) => {
  dispatch({type: ActionType.WORKER_FETCHING, key: workerName})

  try {
    const response = await apiGet(`/api/v1/workers/${workerName}`)

    dispatch({type: ActionType.WORKER_FETCHED, key: workerName, response})
  } catch (error) {
    dispatch({type: ActionType.WORKER_FETCH_FAILED, key: workerName, error})
  }
}

export default fetchWorker
