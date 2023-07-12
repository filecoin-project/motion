# :motorcycle: motion

Motion is a service to propel data onto FileCoin network via a simple easy to use API. It aims to create an easy path for independent software vendors to integrate FileCoin as a storage layer.

## Run Server Locally

### Prerequisites

* Docker container runtime (or your favourite container runtime). The reminder of this README assumes `docker`.
* `curl` (or your favourite HTTP client). The reminder of this README assumes `curl`

### Start Motion API

To start the motion API server run:

```shell
docker run --rm -p 40080:40080 ghcr.io/filecoin-project/motion:main
```
The above starts the Motion HTTP API exposed on default listen address: http://localhost:40080.
It uses a temporary directory to store blobls in a flat file format.

### Store blobs

To store an example blob, use the following `curl` command :
```shell
echo "fish" | curl -X POST -H "Content-Type: application/octet-stream" -d @- http://localhost:40080/v0/blob
```
The response should include a blob ID which you can then use the fetch the blob back. Example:
```json
{"id":"ad7ef987-a932-495c-aa0c-7ffcabeda45f"}
```

### Retrieve a stored blob

To retrieve a stored blob, send a `GET` request to the Motion API with the desired blob ID.
The following command retrieves the blob stored earlier:

```shell
curl http://localhost:40080/v0/blob/ad7ef987-a932-495c-aa0c-7ffcabeda45f
```
This should print the content of the blob on the terminal:

```
fish
```

Alternatively, you can browse the same URL in a web browser, which should prompt you to download the binary file.

### Check the status of an uploaded blob

Not yet implemented.

## API Specification

See the [Motion OpenAPI specification](openapi.yaml).

## Status
:construction: This project is currently under active development.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
