import ActionType from './ActionType.js'
import {apiGet, apiPost} from '../utils/api.js'

// Dispatches that stopped making progress. The list defaults to unresolved
// ones -- see DeadLetters.jsx -- because the question this page answers is
// "what is still broken", and a view that fills up with failures somebody
// already handled stops being read.
export const fetchDeadLetters = (searchQuery) => async (dispatch) => {
  dispatch({type: ActionType.DEAD_LETTERS_FETCHING})

  try {
    const query = searchQuery ? `?${searchQuery}` : ''
    const response = await apiGet(`/api/v1/dispatch-failures${query}`)

    dispatch({type: ActionType.DEAD_LETTERS_FETCHED, response})
  } catch (error) {
    dispatch({type: ActionType.DEAD_LETTERS_FETCH_FAILED, error})
  }
}

// retryDeadLetter asks for a new run. The failed one is never reopened, so the
// response names a different run than the one that failed -- which is why the
// caller is given it rather than just a success flag.
export const retryDeadLetter = (name) => async (dispatch) => {
  dispatch({type: ActionType.DEAD_LETTER_ACTING, name})

  try {
    const response = await apiPost(`/api/v1/dispatch-failures/${name}/retry`, {})

    dispatch({type: ActionType.DEAD_LETTER_ACTED, name, response})

    return response
  } catch (error) {
    dispatch({type: ActionType.DEAD_LETTER_ACTION_FAILED, name, error})

    throw error
  }
}

// resolveDeadLetter closes a failure without scheduling anything.
export const resolveDeadLetter = (name) => async (dispatch) => {
  dispatch({type: ActionType.DEAD_LETTER_ACTING, name})

  try {
    const response = await apiPost(`/api/v1/dispatch-failures/${name}/resolve`, {})

    dispatch({type: ActionType.DEAD_LETTER_ACTED, name, response})

    return response
  } catch (error) {
    dispatch({type: ActionType.DEAD_LETTER_ACTION_FAILED, name, error})

    throw error
  }
}

export default fetchDeadLetters
