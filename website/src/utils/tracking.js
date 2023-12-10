import { useState, useCallback } from 'react';
import { isEmpty } from './objects.js';

class Tracker {
  constructor() {
    this.initialValues = {}
    this.currentValues = {}
  }

  get changed() {
    return !isEmpty(this.currentValues)
  }

  reset() {
    this.initialValues = {...this.initialValues, ...this.currentValues}
    this.currentValues = {}
  }

  bind() {
    this.currentId = 0
  }

  register(state) {
    const id = this.currentId++

    if (!this.initialValues.hasOwnProperty(id)) {
      this.initialValues[id] = state
    }

    return id
  }

  track(id, value) {
    if (this.initialValues[id] !== value) {
      this.currentValues[id] = value
    } else {
      delete this.currentValues[id]
    }
  }
}

export const useTracker = () => {
  const [tracker] = useState(() => new Tracker())
  tracker.bind()

  return tracker
}


export const useTrackedState = (tracker, initialState) => {
  const [state, setState] = useState(initialState)
  const id = tracker.register(state)

  const setTrackedState = useCallback((newState) => {
    setState(newState)
    tracker.track(id, newState)
  }, [])

  return [state, setTrackedState]
}
