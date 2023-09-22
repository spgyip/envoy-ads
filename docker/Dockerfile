FROM debian:bullseye-slim

RUN mkdir -p /app/

WORKDIR /app
COPY bin/ads .

CMD ["./ads", "-l", ":19000"]

