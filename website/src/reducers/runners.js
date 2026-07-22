import ActionType from '../actions/ActionType.js'

const initialState = {}

// Note: runners previously shared the scenarios slice, so the two lists
// overwrote each other on navigation.
export default (state = initialState, action = {}) => {
  switch (action.type) {
    case ActionType.RUNNERS_FETCHING:
      return {...state, fetching: true, error: null}

    case ActionType.RUNNERS_FETCHED:
      return {...state, fetching: false, error: null, response: action.response}

    case ActionType.RUNNERS_FETCH_FAILED:
      return {...state, fetching: false, error: action.error}

    default:
      return state
  }
}
