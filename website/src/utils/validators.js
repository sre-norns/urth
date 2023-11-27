export const validateNotEmpty = (value, prevValue, force) => {
  if (typeof value === 'string' && value.trim().length === 0 && (force || value !== prevValue)) {
    return 'This field is required.'
  }
}

export const validateMaxLength = (max) => (value) => {
  if (typeof value === 'string' && value.length > max) {
    return `This field must be no more than ${max} characters. Currently ${value.length} characters.`
  }
}
