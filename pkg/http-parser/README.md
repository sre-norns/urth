# HTTP / REST request tester parser

A simple parser that takes `.http` [Idea's HTTP Client](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html) files and converts them into 
`http.Request`-compatible object.
This library is intended to help building test script runners as part of the Urth project.

## Usage
```golang

import "github.com/sre-norns/urth/pkg/httpparser"

...

    requests, err := httpparser.Parse(scriptFile)
    if err != nil {
        ... // Handle parsing error
    }

    for _, request := range request {
        ... // Do something with requests
        res, err := httpClient.Do(request)
        ...
    }
..

```