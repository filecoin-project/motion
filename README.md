# :motorcycle: motion

Motion is a service to propel data onto Filecoin network via a simple easy to use API. It aims to create an easy path for independent software vendors to integrate Filecoin as a storage layer.

## Usage

```text
$ motion --help
NAME:
   motion - Propelling data onto Filecoin

USAGE:
   motion [global options] command [command options] [arguments...]

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --dealDuration value                                                         The duration of deals made on Filecoin (default: One year (356 days))
   --dealStartDelay value                                                       The deal start epoch delay. (default: 72 hours)
   --experimentalRemoteSingularityAPIUrl value                                  When using a singularity as the storage engine, if set, uses a remote HTTP API to interface with Singularity (default: use singularity as a code library)
   --experimentalSingularityStore                                               Whether to use experimental Singularity store as the storage and deal making engine (default: Local storage is used)
   --help, -h                                                                   show help
   --localWalletDir value                                                       The path to the local wallet directory. (default: Defaults to '<user-home-directory>/.motion/wallet' with wallet key auto-generated if not present. Note that the directory permissions must be at most 0600.) [$MOTION_LOCAL_WALLET_DIR]
   --localWalletGenerateIfNotExist                                              Whether to generate the local wallet key if none is found (default: true)
   --pricePerDeal value                                                         The maximum price per deal in attoFIL. (default: 0)
   --pricePerGiB value                                                          The maximum  price per GiB in attoFIL. (default: 0)
   --pricePerGiBEpoch value                                                     The maximum price per GiB per Epoch in attoFIL. (default: 0)
   --replicationFactor value                                                    The number of desired replicas per blob (default: Number of storage providers; see 'storageProvider' flag.)
   --storageProvider value, --sp value [ --storageProvider value, --sp value ]  Storage providers to which to make deals with. Multiple providers may be specified. (default: No deals are made to replicate data onto storage providers.)
   --storeDir value                                                             The path at which to store Motion data (default: OS Temporary directory) [$MOTION_STORE_DIR]

   Lotus

   --lotusApi value    Lotus RPC API endpoint (default: "https://api.node.glif.io/rpc/v1") [$LOTUS_API]
   --lotusToken value  Lotus RPC API token [$LOTUS_TOKEN]
```

## Run Server Locally

### Prerequisites

* Docker container runtime (or your favourite container runtime). The remainder of this README assumes `docker`.
* `curl` (or your favourite HTTP client). The reminder of this README assumes `curl`

### Start Motion API

To start the motion API server run:

```shell
docker run --rm -p 40080:40080 ghcr.io/filecoin-project/motion:main
```
The above starts the Motion HTTP API exposed on default listen address: http://localhost:40080.
It uses a temporary directory to store blobs in a flat file format.

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

### Configure local store directory

To configure the local storage directory, set `--storeDir` flag as an argument to the container.
This will override the directy path used by Motion to store files locally.
The path can be a [mounted volume](https://docs.docker.com/storage/volumes/), which then allows you to retain the stored files after the container
is restarted and restore them back. For example, the following command uses a directory named `store` in the current working directory mounted as a volume on Motion container at path `/store`:

```shell
docker run --rm -p 40080:40080 -v $PWD/store:/store ghcr.io/filecoin-project/motion:main --storeDir=/store
```

### Check the status of an uploaded blob

Not yet implemented.

## API Specification

See the [Motion OpenAPI specification](openapi.yaml).

## Status

:construction: This project is currently under active development.

## Local Development

To set up `filecoin-ffi` dependencies, run:

```shell
make build
```

This is only necessary to run once. After that you can use the regular `go build` command to build Motion from source.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
