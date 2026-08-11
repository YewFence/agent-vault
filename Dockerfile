FROM debian:13-slim AS builder

RUN apt-get update  \
    && apt-get -y --no-install-recommends install  \
        # install any other dependencies you might need
        sudo curl git ca-certificates build-essential \
    && rm -rf /var/lib/apt/lists/*

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ENV MISE_DATA_DIR="/mise"
ENV MISE_CONFIG_DIR="/mise"
ENV MISE_CACHE_DIR="/mise/cache"
ENV MISE_INSTALL_PATH="/usr/local/bin/mise"
ENV PATH="/mise/shims:$PATH"

RUN curl https://mise.run | sh

WORKDIR /src
COPY mise.toml mise.lock ./
ENV MISE_LOCKED="1"
ENV MISE_SAFE="1"
RUN mise install

COPY web/package.json web/package-lock.json ./web/
RUN aube -C web ci
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN aube -C web run build

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w \
    -X github.com/Infisical/agent-vault/cmd.version=${VERSION} \
    -X github.com/Infisical/agent-vault/cmd.commit=${COMMIT} \
    -X github.com/Infisical/agent-vault/cmd.date=${BUILD_DATE}" \
    -o /agent-vault .

# ---- Runtime stage ----
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates \
    && addgroup -S agentvault && adduser -S -G agentvault -u 65532 agentvault \
    && mkdir -p /data/.agent-vault && chown -R agentvault:agentvault /data

COPY --from=builder /agent-vault /usr/local/bin/agent-vault
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV HOME=/data
VOLUME /data
EXPOSE 14321
USER agentvault

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:14321/health || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["server", "--host", "0.0.0.0", "--port", "14321"]
