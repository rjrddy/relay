FROM node:22-bookworm-slim AS web
WORKDIR /app/apps/web
COPY apps/web/package*.json ./
RUN npm ci
COPY apps/web/ ./
RUN npm run build
FROM golang:1.27.3-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
COPY sdk/go/ sdk/go/
RUN CGO_ENABLED=0 go build -trimpath -o /relay ./cmd/relay
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /relay /app/relay
COPY --from=web /app/apps/web/dist /app/web
ENV RELAY_WEB=/app/web
EXPOSE 8080
ENTRYPOINT ["/app/relay"]
CMD ["serve"]
