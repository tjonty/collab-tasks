FROM golang:1.27-alpine AS build
WORKDIR /src

# Download modules before copying the source so this layer stays cached
# until go.mod or go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

FROM gcr.io/distroless/static-debian12:nonroot
# /seed creates a demo project: docker compose exec api /seed
COPY --from=build /out/api /out/seed /
EXPOSE 8080
ENTRYPOINT ["/api"]
