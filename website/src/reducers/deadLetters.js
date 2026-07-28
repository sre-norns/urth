import ActionType from '../actions/ActionType.js'

const initialState = {acting: {}}

// `acting` is keyed by failure name rather than a single boolean, so that
// retrying one row disables that row's buttons and not the whole table.
export default (state = initialState, action = {}) => {
  switch (action.type) {
    case ActionType.DEAD_LETTERS_FETCHING:
      return {...state, fetching: true, error: null}

    case ActionType.DEAD_LETTERS_FETCHED:
      return {...state, fetching: false, error: null, response: action.response}

    case ActionType.DEAD_LETTERS_FETCH_FAILED:
      return {...state, fetching: false, error: action.error}

    case ActionType.DEAD_LETTER_ACTING:
      return {...state, actionError: null, acting: {...state.acting, [action.name]: true}}

    case ActionType.DEAD_LETTER_ACTED: {
      const acting = {...state.acting}
      delete acting[action.name]

      return {...state, acting, lastAction: {name: action.name, response: action.response}}
    }

    case ActionType.DEAD_LETTER_ACTION_FAILED: {
      const acting = {...state.acting}
      delete acting[action.name]

      return {...state, acting, actionError: action.error}
    }

    default:
      return state
  }
}
