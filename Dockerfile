FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
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
ENV IMAGE_HOST_PROVIDER=http-json
ENV IMAGE_HOST_UPLOAD_URL=https://2bad.lujilujilujilujiluji.com/
ENV IMAGE_HOST_AUTH_HEADER=Authorization
ENV IMAGE_HOST_AUTH_VALUE="Bearer cooper"
ENV IMAGE_HOST_FIELD_NAME=file
ENV IMAGE_HOST_RESPONSE_URL_PATH=url
EXPOSE 8080
CMD ["/app/image-web"]
