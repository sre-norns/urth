import React, {Component} from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import FormContext from './FormContext.js'
import FormGroupContext from './FormGroupContext.js'

const FormGroupContainer = styled.div``

class FormGroup extends Component {
  static contextType = FormContext

  constructor(props) {
    super(props)

    this.state = {
      context: {
        controlId: props.controlId,
        error: undefined,
        validate: this.onValidate,
      },
    }

    this.value = undefined
  }

  componentDidMount() {
    if (this.context && this.context.register && this.props.controlId) {
      this.context.register(this.props.controlId, this.onForceValidate)
    }
  }

  componentWillUnmount() {
    if (this.context && this.context.unregister && this.props.controlId) {
      this.context.unregister(this.props.controlId)
    }
  }

  componentDidUpdate(prevProps, prevState, snapshot) {
    if (prevProps.controlId !== this.props.controlId) {
      if (this.context && this.context.unregister && prevProps.controlId) {
        this.context.unregister(prevProps.controlId)
      }

      if (this.context && this.context.register && this.props.controlId) {
        this.context.register(this.props.controlId, this.onForceValidate)
      }

      this.setState((state) => ({
        context: {
          ...state.context,
          controlId: this.props.controlId,
        },
      }))
    }
  }

  notifyValidated(value, error) {
    this.setState((state) => ({
      context: {
        ...state.context,
        error,
      },
    }))

    if (this.context && this.context.validated && this.props.controlId) {
      this.context.validated(this.props.controlId, error)
    }
  }

  onValidate = (value, prevValue, force) => {
    this.value = value

    const {onValidate} = this.props
    if (typeof onValidate === 'function') {
      const result = onValidate(value, prevValue, force)
      if (result instanceof Promise) {
        result.then((error) => {
          this.notifyValidated(value, error)
        })
      } else {
        this.notifyValidated(value, result)
      }
    } else {
      this.notifyValidated(value, undefined)
    }
  }

  onForceValidate = () => {
    this.onValidate(this.value, this.value, true)
  }

  render() {
    const {controlId, onValidate, children, ...props} = this.props

    return (
      <FormGroupContext.Provider value={this.state.context}>
        <FormGroupContainer {...props}>{children}</FormGroupContainer>
      </FormGroupContext.Provider>
    )
  }
}

FormGroup.propTypes = {
  controlId: PropTypes.string,
  onValidate: PropTypes.func,
}

export default FormGroup
