FROM golang:1.20-bullseye as build

WORKDIR /go/src/motion

RUN apt-get update && apt-get install -y libhwloc-dev ocl-icd-opencl-dev jq
COPY Makefile .
RUN make extern/filecoin-ffi

COPY go.* .
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /go/bin/motion ./cmd/motion

FROM gcr.io/distroless/base-debian11
COPY --from=build /go/bin/motion /usr/bin/
COPY --from=build /go/src/motion/extern/filecoin-ffi/filcrypto.h \
                  /go/src/motion/extern/filecoin-ffi/filcrypto.pc \
                  /go/src/motion/extern/filecoin-ffi/libfilcrypto.a \
                  /usr/lib/*/libhwloc.so.15 \
                  /usr/lib/*/libOpenCL.so.1 \
                  /usr/lib/*/libudev.so.1 \
                  /lib/*/libgcc_s.so.1 \
                  /usr/lib/

ENTRYPOINT ["/usr/bin/motion"]