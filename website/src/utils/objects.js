export const deepEqual = (a, b, seen = new Map()) => {
  if (seen.has(a)) {
    return seen.get(a) === b
  }

  seen.set(a, b)

  if (a instanceof Date && b instanceof Date) {
    return a.getTime() === b.getTime()
  }

  if (typeof a !== 'object' || a === null || typeof b !== 'object' || b === null) {
    return a === b
  }

  if (Object.keys(a).length !== Object.keys(b).length) {
    return false
  }

  for (let key in a) {
    if (!(key in b) || !deepEqual(a[key], b[key], seen)) {
      return false
    }
  }

  return true
}

export const isEmpty = (obj) => {
  if (obj === null || obj === undefined) {
    return true
  }

  for (let key in obj) {
    if (obj.hasOwnProperty(key)) {
      return false
    }
  }
  return true
}
