FROM golang:1.21-bullseye as build

WORKDIR /go/src/motion

COPY go.* .
COPY integration/ribs/go.* integration/ribs/
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -o /go/bin/motion ./cmd/motion

FROM gcr.io/distroless/static-debian12
COPY --from=build /go/bin/motion /usr/bin/

ENTRYPOINT ["/usr/bin/motion"]
