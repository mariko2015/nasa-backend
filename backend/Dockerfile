FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/backend .

FROM alpine:3.21
RUN adduser -D -H -u 10001 app
COPY --from=build /out/backend /app/backend
USER app
EXPOSE 8080
ENTRYPOINT ["/app/backend"]
