FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY app /usr/bin/app
ENTRYPOINT ["/usr/bin/app"]
CMD ["serve"]
