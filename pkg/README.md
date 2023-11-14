# Sharable packages for Urth service

* [grace](./grace/README.md) - A shared non-domain specific package providing reusable component for service initialization and graceful shutdown.
* [wyrd](./wyrd/README.md) - Shared non-domain specific package that implements labels and matchers.
* [http-parser](./http-parser/README.md) In implementation of `.http`/`.rest` file format parser. This format is supported by VSCode and IntelliJ IDEA plugins to test rest API by sending hand crafted HTTP request. The parser parses data and produces `http.Request`-compatible object that can be used to actually perform http requests.
* [runner](./runner/) - Implementation of different script type executors. Basic set includes: TCP prob, HTTP Request, and Puppeteer scenario. Future work: provide extensibility via plugins.
* [urth](./urth/) - Domain specific types and implementations. Defiles main domain objects like `Scenario`, `RunResults` and `Runner`.
* [redqueue](./redqueue/) - Shared package for Redis based implementation of `Runners` and `Scheduler`.
