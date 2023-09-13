# :motorcycle: Filecoin REST API

*Accelerating data onto Filecoin!*

## Table of Contents

- [Background](#background)
- [Install](#install-and-setup)
- [Usage](#usage)
- [API Specification](#api-specification)
- [Status](#status)
- [Local Development](#local-development)
- [License](#license)

## Background

The Filecoin REST API is an interface to easily propel data onto the Filecoin network. The REST API is implemented here in a service named 'motion'. This service aims to create an easy path for independent software vendors to integrate Filecoin as a storage layer.

## Install and setup

### Prerequisites

1. A list of storage providers who have agreed to store data for you on Filecoin, with whom you've obtained contracts to specify terms of storage. Work with a Motion account manager to help set these up. If you are simply testing, your account manager can offer sample providers to test with.

2. A Filecoin wallet to make deals with, for which you are in possession of the private key. Various options for obtaining a wallet can be found here (https://docs.filecoin.io/basics/assets/wallets/). Your Motion account manager may be able to offer a test wallet to use during a testing phase.

3. A server (bare metal or VM) to run Motion on, with Docker Engine installed on that server with the Docker Compose plugin included. Recommended hardware requirements for servers are:
- Because we run docker, Linux variants are preferred for the OS
- Recommend at least >500GB disk space available for staging data. The complete Filecoin deal making process takes up to 3 days, and you will need to hold all data until deal making is complete. So the amount of free space you will need is roughly the amount of data you want to onboard per day times 3.
- In general, we do not believe Motion is processor or memory intensive, but a machine with at least 32GB of RAM is optimal
- You will also need `git` installed on this machine.
- The server must have a static IP and/or a domain name that points to it, with a port open so you can transfer data from Motion to Filecoin storage providers

### Setting up motion for the first time

Start by cloning this repository:

```shell
git clone https://github.com/filecoin-project/motion.git
```

Before you can run motion, you must configure it. First, from the motion directory, copy `.env.example` to `.env`:

```shell
cp .env.example .env
```

Now open `.env` and set the required values for your instance of motion. At minimum, you need to set the following values (excerpt from `.env.example` file):

```yaml
# Comma seperated list of storage providers motion should make storage deals with
# You must set this value to contain at least one storage provider for motion to
# work
MOTION_STORAGE_PROVIDERS=

# The private key of the wallet you will use with motion, in hexadecimal format.
# This is the output of `lotus wallet export ~address~` if you are using lotus
# If you are obtaining a wallet through another method follow your wallet providers
# instructions to get your wallet's provider key
MOTION_WALLET_KEY=

# This is the domain/IP you will expose publicly to transfer data to storage providers
# When you initialize the singularity docker setup, it will start a server to deliver
# content to storage providers on localhost:7778. However, you will need to either 
# open this specific port on your firewall, and set this value to http://~your-static-ip~:7778
# or you will need to setup reverse proxying from a dedicated web server like NGinx
SINGULARITY_CONTENT_PROVIDER_DOMAIN=
```

Again, work with your Motion account manager to choose your storage providers, obtain a wallet, and for assistance setting up your server to expose a port for data transfers publicly on your server.

As needed, you can also set additional values in your motion `.env`` file for more custom configurations.

## Usage

Once you've configured your `.env`, you're ready to start motion. From the local repository, simply run 

```shell
docker compose up
```
(if you don't want to see log messages directly in your terminal, run `docker compose up -d`)

You'll know your motion is up an running when you see a message like this in the docker logs:

```
2023-09-07 17:49:57 motion-motion-1                        | 2023-09-08T00:49:57.530Z   INFO    motion/api/server       server/server.go:53  HTTP server started successfully.        {"address": "[::]:40080"}
```

Your copy of motion is now running. The Motion HTTP API is now running on port 40080 of your server's localhost.

### Store blobs
 
To store an example blob, use the following `curl` command :
```shell
echo "fish" | curl -X POST -H "Content-Type: application/octet-stream" -d @- http://localhost:40080/v0/blob
```
The response should include a blob ID which you can then use the fetch the blob back. Example:
```json
{"id":"ad7ef987-a932-495c-aa0c-7ffcabeda45f"}
```

### Storing onto Filecoin

Motion will begin saving data to Filecoin when it's holding at least 16GB of data that hasn't been backed up with a storage provider.

If you want to test storing an actual filecoin deal, the following simple script will put about 20GB of random data into motion:

```shell
for i in {0..20}; do; head -c 1000000000 /dev/urandom | curl -X POST --data-binary @- -H "Content-Type: application/octet-stream" http://localhost:40080/v0/blob; done
```

This should be enough to trigger at least 1 Filecoin deal being made from Motion

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

In addition to retrieving data for a blob, you can also check the status of its storage on Filecoin:

```shell
curl http://localhost:40080/v0/blob/ad7ef987-a932-495c-aa0c-7ffcabeda45f/status | jq .
```
(`jq` being used to pretty print here -- make sure it's installed on your machine. you don't need to pipe to jq but the output will be more readable)

```json
{
  "id": "ad7ef987-a932-495c-aa0c-7ffcabeda45f",
  "Replicas": [
    {
      "provider": "f1234",
      "status": "active",
      "lastVerified": "2020-12-01T22:48:00Z",
      "expiration": "2021-08-18T22:48:00Z"
    }
  ]
}
```

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
