#!/bin/bash

# Create a test file to upload
echo fish > upload.txt

# Create a test bucket, retrying if it fails.
# We do this to ensure we're not failing too early on a connection error.
RETRY_NUM=3
RETRY_EVERY=15
NUM=$RETRY_NUM
until aws --endpoint-url http://127.0.0.1:8000 s3 mb s3://test-bucket
do
  1>&2 echo Retrying $NUM more times
  sleep $RETRY_EVERY
  ((NUM--))

  if [ $NUM -eq 0 ]
  then
    1>&2 echo Bucket was not created after $RETRY_NUM tries
    exit 1
  fi
done 

aws --endpoint-url http://127.0.0.1:8000 s3 cp upload.txt s3://test-bucket
aws --endpoint-url http://127.0.0.1:8000 s3 cp s3://test-bucket/upload.txt download.txt
cmp upload.txt download.txt

# Compare the downloaded file with the original and echo if successful
if cmp upload.txt download.txt
then
  1>&2 echo "Uploaded and downloaded files are the same"
else
  1>&2 echo "Uploaded and downloaded files are different"
  exit 1
fi