FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY hive-server /usr/bin/hive-server
ENTRYPOINT ["/usr/bin/hive-server"]
CMD ["serve"]
