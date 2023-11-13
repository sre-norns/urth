import color from './color/index.js'
import createShade from './shade.js'

export const createTheme = (dark) => ({
  dark,
  color,
  shade: createShade(dark),
})