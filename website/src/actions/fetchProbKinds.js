import ActionType from './ActionType.js'
import { apiGet } from '../utils/api.js'

// The server owns the registry of prob kinds. The UI asks rather than carrying
// its own list, so a kind added to the server appears here without a UI change.
const fetchProbKinds = () => async (dispatch) => {
  dispatch({ type: ActionType.PROB_KINDS_FETCHING })

  try {
    const response = await apiGet('/api/v1/probs')

    dispatch({ type: ActionType.PROB_KINDS_FETCHED, response })
  } catch (error) {
    dispatch({ type: ActionType.PROB_KINDS_FETCH_FAILED, error })
  }
}

export default fetchProbKinds
