FROM node:22-alpine AS frontend
WORKDIR /src/frontend
ARG NPM_REGISTRY=https://registry.npmjs.org/
COPY frontend/package*.json ./
RUN npm config set registry "$NPM_REGISTRY" \
    && npm config set replace-registry-host always \
    && npm ci --no-audit --no-fund --loglevel=info --fetch-retries=3 --fetch-retry-mintimeout=10000 --fetch-retry-maxtimeout=60000
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/image-web ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /out/image-web /app/image-web
COPY --from=frontend /src/frontend/dist /app/static
ENV PORT=8080
EXPOSE 8080
CMD ["/app/image-web"]
