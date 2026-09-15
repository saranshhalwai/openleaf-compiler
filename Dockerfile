# Stage 1: Build the Go compiler orchestrator binary
FROM golang:alpine AS builder

WORKDIR /app
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Stage 2: Sandboxed TeX Live Runtime
FROM debian:bookworm-slim

# Prevent interactive prompts during apt install
ENV DEBIAN_FRONTEND=noninteractive

# Install TeX Live, latexmk, and essential fonts & math packages
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    perl \
    texlive-latex-base \
    texlive-latex-recommended \
    texlive-latex-extra \
    texlive-fonts-recommended \
    texlive-science \
    latexmk \
    && rm -rf /var/lib/apt/lists/*

# Security: Create non-root unprivileged runner user
RUN useradd -m -u 1000 -s /bin/bash latexrunner

WORKDIR /home/latexrunner

# Copy compiled Go server binary from builder
COPY --from=builder /app/server /usr/local/bin/server

# Set ownership and secure permissions
RUN chown -R latexrunner:latexrunner /home/latexrunner

# Run as non-root user
USER latexrunner

ENV PORT=8080
ENV ENV=production

EXPOSE 8080

CMD ["/usr/local/bin/server"]
