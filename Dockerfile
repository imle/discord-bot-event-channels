# builder image
FROM golang:1.17-alpine as builder
WORKDIR /app
RUN CGO_ENABLED=0 GOOS=linux go build -o ./bin/bot .


# generate clean, final image for end users
FROM alpine:latest
COPY --from=builder /app/bin/bot /bot

# executable
ENTRYPOINT [ "/bot" ]
