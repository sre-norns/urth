import React, {forwardRef, useCallback, useMemo} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormGroupContext from './FormGroupContext.js'


const FormGroupContainer = styled.div``

const FormGroup = forwardRef(({controlId, onValidate, ...props}, ref) => {
  const [error, setError] = React.useState(undefined)

  const validate = useCallback((value) => {
    if (typeof onValidate === 'function') {
      const result = onValidate(value)
      if (result instanceof Promise) {
        result.then((error) => {
          setError(error)
        })
      } else {
        setError(result)
      }
    }
  }, [onValidate])

  const context = useMemo(() => ({
    controlId, error, validate,
  }), [
    controlId, error, validate,
  ])

  return (
    <FormGroupContext.Provider value={context}>
      <FormGroupContainer {...props} ref={ref}/>
    </FormGroupContext.Provider>
  )
})

FormGroup.propTypes = {
  controlId: PropTypes.string,
  onValidate: PropTypes.func,
}

export default FormGroup
