FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN go build -mod=mod -o /out/api ./cmd/api && go build -mod=mod -o /out/worker ./cmd/worker

FROM alpine:3.20
RUN adduser -D appuser
WORKDIR /app
COPY --from=build /out/api /app/api
COPY --from=build /out/worker /app/worker
COPY --from=build /src/migrations /app/migrations
USER appuser
EXPOSE 8080
CMD ["/app/api"]
