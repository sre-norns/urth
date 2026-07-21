import ActionType from '../actions/ActionType.js'

const initialState = {}

export default (state = initialState, action = {}) => {
  switch (action.type) {
    case ActionType.RESULTS_FETCHING:
      return { ...state, fetching: true, error: null }

    case ActionType.RESULTS_FETCHED:
      return { ...state, fetching: false, error: null, response: action.response }

    case ActionType.RESULTS_FETCH_FAILED:
      return { ...state, fetching: false, error: action.error }

    default:
      return state
  }
}
