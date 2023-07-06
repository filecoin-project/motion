# :motorcycle: motion

Motion is a service to propel data onto FileCoin network via a simple easy to use API. It aims to create an easy path for independent software vendors to integrate FileCoin as a storage layer.

## Run Server Locally

To run the server locally, execute:
```shell
go run ./cmd/motion
```

Alternatively, run the latest motion as a container by executing:

```shell
docker run --rm ghcr.io/filecoin-project/motion:main
```

The above starts the Motion HTTP API exposed on default listen address: http://localhost:40080

For more information, see the [Motion OpenAPI specification](openapi.yaml).

## Status
:construction: This project is currently under active development.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
