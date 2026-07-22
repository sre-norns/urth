import {apiPut} from '../utils/api.js'
import fetchWorkers from './fetchWorkers.js'

// Pausing is a request of its own rather than a resource update, because a
// worker rewrites its own record whenever it registers; the flag an operator
// sets lives somewhere the worker cannot reach.
const setWorkerPaused = (workerName, paused, runnerName) => async (dispatch) => {
  await apiPut(`/api/v1/workers/${workerName}/paused`, {paused})

  dispatch(fetchWorkers(runnerName))
}

export default setWorkerPaused
