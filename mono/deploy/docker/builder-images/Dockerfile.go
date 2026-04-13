FROM golang:1.22-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates make gcc g++ && \
    rm -rf /var/lib/apt/lists/*
RUN useradd -m -s /bin/bash builder
USER builder
WORKDIR /workspace
