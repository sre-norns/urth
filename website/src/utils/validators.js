export const validateNotEmpty = (value) => {
  if (typeof value === 'string' && value.trim().length === 0) {
    return 'This field is required.'
  }
}

export const validateMaxLength = (max) => (value) => {
  if (typeof value === 'string' && value.length > max) {
    return `This field must be no more than ${max} characters. Currently ${value.length} characters.`
  }
}
