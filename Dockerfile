FROM alpine:latest

WORKDIR /usr/local/bin

EXPOSE 3000

COPY .build/raga-backend .

ENTRYPOINT ["/usr/local/bin/raga-backend"]
