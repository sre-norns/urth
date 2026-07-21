// Every fetch in this app keeps the same per-key shape: { fetching, response,
// error }, indexed by whatever identifies the thing being fetched. The existing
// reducers each spell that out by hand; this builds one from the three action
// types so a new resource does not mean another thirty lines of the same.
const createKeyedFetchReducer = ({ fetching, fetched, failed }) => {
  const initialState = {}

  return (state = initialState, action = {}) => {
    switch (action.type) {
      case fetching:
        return {
          ...state,
          [action.key]: { ...state[action.key], fetching: true, error: null },
        }

      case fetched:
        return {
          ...state,
          [action.key]: { ...state[action.key], fetching: false, error: null, response: action.response },
        }

      case failed:
        return {
          ...state,
          [action.key]: { ...state[action.key], fetching: false, error: action.error },
        }

      default:
        return state
    }
  }
}

export default createKeyedFetchReducer
