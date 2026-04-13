FROM gcc:13-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates cmake ninja-build clang-format && \
    rm -rf /var/lib/apt/lists/*
RUN useradd -m -s /bin/bash builder
USER builder
WORKDIR /workspace
