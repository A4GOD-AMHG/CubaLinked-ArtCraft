FROM golang:1.26.3-alpine AS builder
RUN apk add --no-cache ca-certificates tzdata git
WORKDIR /src
COPY CubaLinked-AuthModule ./CubaLinked-AuthModule
COPY CubaLinked-AssetsS3Module ./CubaLinked-AssetsS3Module
COPY CubaLinked-GeoMapModule ./CubaLinked-GeoMapModule
COPY CubaLinked-QueueModule ./CubaLinked-QueueModule
COPY CubaLinked-CalendarModule ./CubaLinked-CalendarModule
COPY CubaLinked-EWalletModule ./CubaLinked-EWalletModule
COPY CubaLinked-NotificationsModule ./CubaLinked-NotificationsModule
COPY CubaLinked-ReviewsModule ./CubaLinked-ReviewsModule
COPY CubaLinked-BusinessKitModule ./CubaLinked-BusinessKitModule
COPY CubaLinked-ArtCraftModule ./CubaLinked-ArtCraftModule
WORKDIR /src/CubaLinked-ArtCraftModule
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/artcraft-server ./cmd/server

FROM alpine:3.21.3 AS prod
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/artcraft-server .
COPY --from=builder /src/CubaLinked-ArtCraftModule/configs/config.yaml ./configs/config.yaml
EXPOSE 50075
ENTRYPOINT ["./artcraft-server"]
