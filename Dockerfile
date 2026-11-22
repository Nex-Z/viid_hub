FROM node:22-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
RUN CGO_ENABLED=0 go build -trimpath -o /out/viid ./cmd/viid

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /out/viid /usr/local/bin/viid
COPY --from=frontend /src/web/dist ./web/dist
COPY demo/media ./demo/media
USER 65532:65532
ENV LISTEN_ADDR=0.0.0.0:14000
EXPOSE 14000
STOPSIGNAL SIGINT
ENTRYPOINT ["viid"]
