import React from 'react'
import PropTypes from 'prop-types'
import styled from '@emotion/styled'
import NavRow from '../components/NavRow.js'
import NavBrand from '../components/NavBrand.js'
import NavLink from '../components/NavLink.js'
import Nav from '../components/Nav.js'
import NavRowContainer from '../components/NavRowContainer.js'
import TextInput from '../components/TextInput.js'
import { useDebounce } from "use-debounce";

const onNonClick = (e) => {
  e.preventDefault()
}

const SearchInput = styled(TextInput)`
  flex-grow: 1;
`

const SearchTextInput = () => {
  const [searchInput, setSearchInput] = useState('');
  const [debouncedValue] = useDebounce(searchInput, 500);

  const [currentQueryParameters, setSearchParams] = useSearchParams();
  const newQueryParameters = new URLSearchParams();

  const inputHandler = (e) => {
    if (debouncedValue) {
      newQueryParameters.set("labels", debouncedValue);
    }

    console.log("setting search query to", newQueryParameters)
    setSearchParams(newQueryParameters);
    setSearchInput(e.target.value)
  }

  return (<SearchInput
    placeholder="Search"
    value={searchInput}
    onChange={inputHandler}
  />)
}

const HeaderMock = () => (
  <Nav>
    <NavRowContainer size="large">
      <NavRow>
        <NavBrand href="/">Urth</NavBrand>
        <NavLink href="/scenarios" active>
          Scenarios
        </NavLink>
        <NavLink href="#" onClick={onNonClick}>
          Run Results
        </NavLink>
        <NavLink href="#" onClick={onNonClick}>
          Runners
        </NavLink>
        <NavLink href="#" onClick={onNonClick} disabled active>
          Dashboards
        </NavLink>
      </NavRow>
    </NavRowContainer>
    <NavRowContainer size="medium">
      <NavRow center>
        <NavLink href="#" onClick={onNonClick}>
          Active
        </NavLink>
        <NavLink href="#" onClick={onNonClick}>
          Disabled
        </NavLink>
        <NavLink href="#" onClick={onNonClick} active>
          All
        </NavLink>
        <SearchTextInput />
      </NavRow>
    </NavRowContainer>
  </Nav>
)

export default HeaderMock
