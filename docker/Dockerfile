FROM debian:bookworm-slim

ARG PI_VERSION=v0.74.0
ARG GO_VERSION=1.24.3
ARG NODE_VERSION=24.15.0

ENV DEBIAN_FRONTEND=noninteractive

# Create non-root user
RUN groupadd -g 1000 agent && useradd -m -u 1000 -g agent agent

# Minimal runtime deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    xz-utils \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Go
RUN curl -fsSL "https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz" \
    | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/home/agent/go"
ENV HOME="/home/agent"
ENV PATH="${GOPATH}/bin:${PATH}"

# Node.js
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
    | tar -C /usr/local -xJ --strip-components=1

# pi coding agent — install to /opt/pi, symlink binary to PATH
RUN curl -fsSL "https://github.com/earendil-works/pi/releases/download/${PI_VERSION}/pi-linux-x64.tar.gz" \
    | tar -C /opt -xz \
    && ln -s /opt/pi/pi /usr/local/bin/pi

RUN mkdir -p /workspace && chown -R agent:agent /workspace
WORKDIR /workspace

USER agent
