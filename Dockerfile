FROM alpine:3.22

RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
COPY api-gateway-controller /bin/api-gateway-controller

ENTRYPOINT ["/bin/api-gateway-controller"]
