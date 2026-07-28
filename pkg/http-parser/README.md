# HTTP / REST request tester parser

A parser that takes `.http` / `.rest` files — the format used by
[IntelliJ's HTTP Client](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html)
— and converts them into `http.Request`-compatible objects.
For a CLI client see https://www.jetbrains.com/help/idea/http-client-cli.html

This library is intended to help building test script runners as part of the
Urth project: `pkg/probers/rest` embeds one of these scripts in a scenario
manifest and executes the requests it describes, in order.

**Only the shape of the request is parsed.** A script's response handlers
(`> {% ... %}`) are JavaScript assertions run by the IDE. They are recognised so
they cannot be mistaken for a request body, and then discarded — deciding
whether a response is acceptable is the prober's job, not the parser's.

## Usage

```golang

import httpparser "github.com/sre-norns/urth/pkg/http-parser"

...

    requests, err := httpparser.Parse(scriptFile)
    if err != nil {
        ... // Handle parsing error
    }

    for _, request := range requests {
        ... // Do something with requests
        res, err := httpClient.Do(request.Request)
        ...
    }
..

```

## Syntax

### Requests

A script is a sequence of blocks, one per request, each of the form:

```http
### Optional title
# @name OptionalName
METHOD target HTTP/version
Header-Name: header value

request body, if any
```

Only the target is required. Everything on the request line except the target is
optional: `GET` is assumed when no method is given, and the protocol version
defaults to HTTP/1.1.

* Simplest form — a `GET` to a URL:

```http
GET http://localhost:8080/api/v1/version
```

Or, shorter still, the bare URL. A **bare URL is a whole request**, so a script
may list endpoints one per line — this is a convenience beyond the IDE's format,
and it is why a request that is nothing but a URL cannot carry a body: give it a
method, and the usual rules apply.

```http
localhost:8080/api/v1/version
go.dev/doc
```

* `POST` with a JSON body. The body is everything after the blank line that ends
  the headers, kept byte for byte up to the next `###` or the end of the script:

```http
POST https://httpbin.org/post
Content-Type: application/json

{
  "name": "John Doe",
  "role": "Developer"
}
```

* Several requests in one file, separated by `###`:

```http
### Request Separation & Title
# @name GetUserProfile
GET {{base_url}}/api/users/1
Authorization: Bearer {{auth_token}}
Accept: application/json

### POST Request Example with Body
POST https://httpbin.org/post
Content-Type: application/json

{
  "name": "John Doe",
  "role": "Developer"
}
```

### Elements

* `###` **separator**: divides requests within a single file. Any text following
  it titles the request that comes after.
* `# @name` **identifier**: names the request. Both `# @name Foo` and
  `# @name = Foo` are accepted, and the name is reported in the run log so a
  failure can be traced to a step. Other `@` directives (`@no-redirect`,
  `@timeout`, ...) describe how the IDE *runs* a request rather than what the
  request is, and are ignored rather than rejected.
* **Headers**: `Name: value` pairs on the lines directly below the request line,
  with no blank line between. Whitespace around the name is tolerated.
* **Body**: separated from the headers by exactly one blank line.
* **Comments**: a line starting with `#` or `//`. A trailing `#` comment is also
  accepted on a request line or a header, provided whitespace precedes the `#`
  — which is what keeps a URL fragment (`/app#/route`) intact. Inside a body
  nothing is treated as a comment, since there the text is payload.
* **Variables**: `@name = value` declares one; `{{name}}` substitutes it into a
  target, a header value or a body. Declarations are script scoped and must
  appear before the request that uses them; a declaration may refer to an
  earlier one.

### Resolving the target

The scheme, host and port come from the request line and the `Host` header
together:

| Script | Request |
| --- | --- |
| `GET https://go.dev/x` | as written |
| `GET go.dev/x` | `https://go.dev/x` — https is assumed |
| `GET go.dev:80/x` | `http://go.dev:80/x` — the cleartext port is named |
| `GET /x` + `Host: go.dev` | `https://go.dev/x` — the header names the server |
| `GET http://10.0.0.7:8080/x` + `Host: api.example.com` | dials `10.0.0.7:8080`, asks for `api.example.com` |

A `Host` header never survives into `Request.Header`: Go writes `Request.Host`
and ignores `Header["Host"]`, so a header left in the map would be dropped
silently. A request line naming only a path, with no `Host` header to say which
server the path is on, is an error — the alternative is a probe that fails at
connect time for reasons that have nothing to do with the service.

## Not implemented

These are rejected with an error rather than ignored, because ignoring them
would change what a request does without saying so:

* **Response handlers and assertions** (`> {% ... %}`) are skipped, not run. A
  script that relies on one to decide pass/fail will report success on any
  response the prober itself accepts.
* **External body files** (`< ./body.json`): a probe script is a string in a
  scenario manifest, with no directory to resolve the path against. Inline the
  body.
* **Dynamic variables** (`{{$uuid}}`, `{{$timestamp}}`, ...): generated by the
  IDE at send time.
* **Undeclared variables**: `{{base_url}}` with no declaration would otherwise
  become a request to a host literally named `{{base_url}}`, reported as a DNS
  failure rather than as the typo it is.
