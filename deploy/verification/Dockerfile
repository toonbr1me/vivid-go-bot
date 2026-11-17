FROM golang:1.27.4-alpine
RUN apk add --no-cache build-base nodejs python3 postgresql-client
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=1 REQUIRE_TEST_DATABASE=true DATABASE_URL=""
CMD ["sh", "-ec", "go test -race -count=1 ./...; go vet ./...; node --test internal/bot/widgets/interface.test.cjs internal/web/static/ticket-live.test.cjs internal/web/static/checkout.test.cjs internal/web/static/miniapp.test.cjs"]
