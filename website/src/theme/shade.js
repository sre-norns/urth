const createShade = (dark) => dark ? {
  background: 'hsl(210, 3%, 7%)',
  text: 'hsl(210, 3%, 93%)',
} : {
  background: 'hsl(210, 3%, 93%)',
  text: 'hsl(210, 5%, 15%)',
}

export default createShade