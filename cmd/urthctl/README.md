# Urth command line utility

Command line utility to interact with a Prober Platform `Urth`.
It allows user to run test scripts locally in exactly the same way a script running job would execute them.


# Usage

To get the full list of supported subcommand use `--help` option.
```shell
> go run ./cmd/urthctl --help
```

To run a test script:
```shell
> go run ./cmd/urthctl run <scrupt file>
```

Note that running a script from the STDIN is also supported, but a `--kind` hint must be provided to tell the tool which type of script is being run.

For example, `urthctl` can replay [HAR](https://en.wikipedia.org/wiki/HAR_(file_format)) files saved from a web-borwser:
```shell
> go run ./cmd/urthctl run ./website.har
```

Or it can convert HAR file into a .HTTP file, as supported by [VSCode](https://marketplace.visualstudio.com/items?itemName=humao.rest-client) and [IntelliJ IDEA](https://www.jetbrains.com/help/idea/http-client-in-product-code-editor.html): 
```shell
> go run ./cmd/urthctl convert ./website.har 
```
