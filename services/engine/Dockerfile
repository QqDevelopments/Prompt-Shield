FROM rust:1.90-bookworm AS build
WORKDIR /source
COPY proto proto
COPY services/engine services/engine
WORKDIR /source/services/engine
RUN cargo build --locked --release --features ner

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 && rm -rf /var/lib/apt/lists/* && useradd --system --uid 10001 shield
COPY --from=build /source/services/engine/target/release/prompt-shield-engine /usr/local/bin/prompt-shield-engine
USER 10001
ENV ENGINE_LISTEN=0.0.0.0:50051
EXPOSE 50051
ENTRYPOINT ["prompt-shield-engine"]
