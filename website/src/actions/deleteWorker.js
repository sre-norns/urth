import {apiDelete} from '../utils/api.js'
import fetchWorkers from './fetchWorkers.js'

// Revokes a worker's registration. The worker still holds its runner token, so
// it can register again unless the runner is disabled -- this drops a worker,
// it does not bar it.
const deleteWorker = (worker, runnerName) => async (dispatch) => {
  const {uid, version} = worker.metadata

  await apiDelete(`/api/v1/workers/${uid}?version=${version}`)

  dispatch(fetchWorkers(runnerName))
}

export default deleteWorker
