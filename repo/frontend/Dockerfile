FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN go build -mod=mod -o /out/web ./cmd/web

FROM alpine:3.20
RUN adduser -D appuser
WORKDIR /app
COPY --from=build /out/web /app/web
COPY --from=build /src/ui/static /app/ui/static
USER appuser
EXPOSE 8081
CMD ["/app/web"]
