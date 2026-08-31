# syntax=docker/dockerfile:1

FROM golang:1.26.7-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/gateway /gateway

USER nonroot:nonroot
EXPOSE 8770

ENTRYPOINT ["/gateway"]
