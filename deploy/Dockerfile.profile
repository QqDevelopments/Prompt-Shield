FROM rust:1.90-bookworm AS build
WORKDIR /source
COPY proto proto
COPY services/engine services/engine
WORKDIR /source/services/engine
ENV CARGO_PROFILE_RELEASE_DEBUG=1
RUN cargo build --locked --release --features evaluation --bin allocation-profile

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends heaptrack libssl3 ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /source/services/engine/target/release/allocation-profile /allocation-profile
