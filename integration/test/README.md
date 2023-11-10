# Motion Devnet Integration Test Suite

This integration allows you to spin up a complete devnet including Lotus, Boost, Singularity, and Motion, in order to test round trip deal flow. It includes an integration test that covers all steps of a motion deal workflow, including storing to a filecoin provider and retrieving data back via trustless HTTP retrieval.

It's intended both as an integration test to run in CI, and a platform for developers to spin up test nets so they can try out new Motion features they are working on.

### Command reference

All devnets are spun up with `make`. Key commands:

- `make all/up` - starts all test components
- `make devnet/up` - will spin up a boost / lotus devnet
- `make motionlarity/up` - will spin up motion and singularity pre-configured to make deals with a lotus/boost devnet. if a lotus / boost devnet is not already spun up, this will spin one up
- `make s3-connector/up` - will start the s3 connector, as well as motionlarity and devnet if not already running
- `make test` - this will run the integration test on top of the motion/singularity/boost/lotus devnets. It will spin up all required networks that are not already running
- `make s3-connector-down` - will shut down s3 connector (motionlarity and devnet will NOT be stopped)
- `make motionlarity/down` - this will shut down motion and singularity processes, as well as the s3 connector if running (devnet will NOT be stopped)
- `make devnet/down` - this will shut down the boost/lotus devnet. If singularity and motion processes running, it will shut them down as well
- `make all/down` - stops all test components

### Testing with S3 CLI

Set the following environment variables to point AWS CLI to the test server:

```
export AWS_ACCESS_KEY_ID=accessKey1
export AWS_SECRET_ACCESS_KEY=verySecretKey1
export AWS_DEFAULT_REGION=location-motion-v1 \
export AWS_ENDPOINT_URL=http://localhost:8000
```

Simple bucket creation / upload / download:

```
aws s3 mb s3://test
aws s3 cp ./myfile s3://test
aws s3 cp s3://test/myfile ./myfile-retrieved
```

### Using local singularity

Motion processes that are spun up with this test suite automatically use the code that is in the local repository on the current branch.

However, Singularity processes are by default spun up with a remote image. If you want to spin up Singularity using a local code repository as the base, you'll want to specify `SINGULARITY_LOCAL_DOCKERFILE` as the path to your local singularity repo. For example, if you store your go repositories on the traditional go src folder hierarchy, you can use:

`SINGULARITY_LOCAL_DOCKERFILE=../../../../data-preservation-programs/singularity  make motionlarity/up`