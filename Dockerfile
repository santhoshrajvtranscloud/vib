# ==============================================================================
# Visual Image Builder (VIB) - Multi-Stage Container Build
# ==============================================================================

# --- Stage 1: Build React Frontend ---
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web

COPY web/package.json web/package-lock.json* ./
RUN npm ci || npm install

COPY web/ ./
RUN npm run build

# --- Stage 2: Build Standalone Go Executable with Embedded Frontend ---
FROM golang:1.24-bookworm AS go-builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY internal/ ./internal/
COPY cmd/ ./cmd/
COPY embed.go ./
COPY --from=frontend-builder /app/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/vib ./cmd/vib

# --- Stage 3: Minimal Production Runtime with Packer & AWS CLI ---
FROM debian:bookworm-slim AS runner

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Install official HashiCorp Packer
ARG PACKER_VERSION=1.11.2
RUN ARCH=$(dpkg --print-architecture) && \
    curl -fsSL "https://releases.hashicorp.com/packer/${PACKER_VERSION}/packer_${PACKER_VERSION}_linux_${ARCH}.zip" -o /tmp/packer.zip && \
    unzip -q /tmp/packer.zip -d /usr/local/bin/ && \
    chmod +x /usr/local/bin/packer && \
    rm -f /tmp/packer.zip

# Install official AWS CLI v2
RUN ARCH=$(dpkg --print-architecture) && \
    if [ "$ARCH" = "amd64" ]; then AWS_ARCH="x86_64"; else AWS_ARCH="aarch64"; fi && \
    curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${AWS_ARCH}.zip" -o /tmp/awscliv2.zip && \
    unzip -q /tmp/awscliv2.zip -d /tmp/ && \
    /tmp/aws/install && \
    rm -rf /tmp/aws /tmp/awscliv2.zip

# Copy standalone VIB binary from builder
COPY --from=go-builder /app/vib /usr/local/bin/vib

# Create VIB home directory
ENV HOME=/root
RUN mkdir -p /root/.vib

WORKDIR /workspace

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/vib"]
CMD ["--serve", "--port", "8080", "--addr", "0.0.0.0:8080"]
