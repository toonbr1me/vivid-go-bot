# Сборка статического бинарника: в образ не попадают ни компилятор, ни
# исходники. Шаблоны и стили уже вшиты через go:embed, поэтому каталог web
# нужен только для переопределений.
FROM golang:1.27.4-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vivid ./cmd/vivid

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 -h /app vivid

WORKDIR /app
COPY --from=build /out/vivid /usr/local/bin/vivid

USER vivid
EXPOSE 8080
ENTRYPOINT ["vivid"]
