import ActionType from '../actions/ActionType.js'

const initialState = {}

export default (state = initialState, action = {}) => {
  switch (action.type) {
    case ActionType.PROB_KINDS_FETCHING:
      return { ...state, fetching: true, error: null }

    case ActionType.PROB_KINDS_FETCHED:
      return { ...state, fetching: false, error: null, response: action.response }

    case ActionType.PROB_KINDS_FETCH_FAILED:
      return { ...state, fetching: false, error: action.error }

    default:
      return state
  }
}
