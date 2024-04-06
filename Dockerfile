FROM alpine:latest

WORKDIR /usr/local/bin

EXPOSE 3000

COPY .build/raga-proxy .

ENTRYPOINT ["/usr/local/bin/raga-proxy"]
