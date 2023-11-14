import React from "react";
import styled from '@emotion/styled'
import Empty from "./Empty.js"

const Container = styled.div`
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;
`

const EmptyInlay = () => (
  <Container>
    <Empty/>
  </Container>
)

export default EmptyInlay