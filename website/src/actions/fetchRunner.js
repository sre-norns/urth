import ActionType from './ActionType.js'
import {apiGet} from '../utils/api.js'

const fetchRunner = (runnerId) => async (dispatch) => {
  dispatch({type: ActionType.RUNNER_FETCHING, key: runnerId})

  try {
    const response = await apiGet(`/api/v1/runners/${runnerId}`)

    dispatch({type: ActionType.RUNNER_FETCHED, key: runnerId, response})
  } catch (error) {
    dispatch({type: ActionType.RUNNER_FETCH_FAILED, key: runnerId, error})
  }
}

export default fetchRunner
