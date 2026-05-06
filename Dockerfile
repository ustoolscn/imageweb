FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend
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
ENV DATA_DIR=/app/data
ENV DATABASE_PATH=/app/data/app.db
ENV STATIC_DIR=/app/static
ENV SCDN_UPLOAD_URL=https://2bad.lujilujilujilujiluji.com/
EXPOSE 8080
VOLUME ["/app/data"]
CMD ["/app/image-web"]
