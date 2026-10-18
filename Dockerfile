# One image, one binary: the SPA and the base config are embedded in the
# server, so the final image holds only the binary, CA certs and /data.

FROM node:26-alpine3.22 AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Only the fresh build is embedded, never whatever sat in public/ locally.
RUN find cmd/server/public -mindepth 1 ! -name .gitkeep -delete
COPY --from=frontend /app/dist/ cmd/server/public/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmphost ./cmd/server
RUN echo 'nobody:x:65534:65534:nobody:/:' > /etc_passwd && mkdir -p /data

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend /etc_passwd /etc/passwd
COPY --from=backend /tmphost /tmphost
COPY --from=backend --chown=65534:65534 /data /data
ENV TMPHOST_SETTINGS_FILE=/data/settings.json \
    TMPHOST_SQLITE_DSN=/data/tmphost.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
VOLUME /data
EXPOSE 3000
USER nobody
CMD ["/tmphost"]
