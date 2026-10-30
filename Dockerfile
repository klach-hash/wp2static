FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /wp2static .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /wp2static /usr/local/bin/wp2static
WORKDIR /data
EXPOSE 8087 8088
ENTRYPOINT ["wp2static"]
CMD ["panel", "-out", "/data", "-addr", "0.0.0.0:8087"]
