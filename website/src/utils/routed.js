import React from 'react'
import {useNavigate} from 'react-router-dom'


const routed = (WrappedComponent, replace = false) => {
  return React.forwardRef((props, ref) => {
    const navigate = useNavigate()
    const handleClick = (e) => {
      if (e.target.href) {
        const url = new URL(e.target.href)
        if (url.origin === window.location.origin) {
          e.preventDefault()
          navigate(url.pathname, {replace})
        }
      }
    }

    return <WrappedComponent ref={ref} onClick={handleClick} {...props} />
  })
}

export default routed
