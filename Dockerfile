FROM golang:1.20-bullseye as build

WORKDIR /go/src/motion
COPY go.* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /go/bin/motion ./cmd/motion

FROM gcr.io/distroless/base-debian11
COPY --from=build /go/bin/motion /usr/bin/

ENTRYPOINT ["/usr/bin/motion"]