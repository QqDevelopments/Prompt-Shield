FROM node:24-bookworm-slim AS console
WORKDIR /console
COPY apps/web-console/package*.json ./
RUN npm ci
COPY apps/web-console ./
RUN npm run build

FROM golang:1.27-bookworm AS build
WORKDIR /source
COPY services/gateway/go.mod services/gateway/go.sum ./
RUN go mod download
COPY services/gateway ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ingress ./cmd/ingress

FROM gcr.io/distroless/static-debian12:nonroot AS ingress
COPY --from=build /ingress /ingress
EXPOSE 8080
ENTRYPOINT ["/ingress"]

FROM gcr.io/distroless/static-debian12:nonroot AS backend
COPY --from=build /gateway /gateway
ENV CONSOLE_ASSETS=/console GATEWAY_LISTEN=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/gateway"]

FROM backend AS combined
COPY --from=console /console/dist /console
