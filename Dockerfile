FROM golang:1.20-bullseye as build

WORKDIR /go/src/motion

RUN apt-get update && apt-get install -y jq

COPY go.* .
COPY integration/ribs/go.* integration/ribs/
RUN go mod download
COPY . .

RUN go build -o /go/bin/motion ./cmd/motion

FROM gcr.io/distroless/base-debian11
COPY --from=build /go/bin/motion /usr/bin/

ENTRYPOINT ["/usr/bin/motion"]