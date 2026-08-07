# Multi-stage build: the Go binary serves the built SPA, so production is one
# container, one Cloud Run service, and same-origin (no CORS anywhere).

# ── Frontend ──────────────────────────────────────────────────────────────────
FROM node:22-alpine AS web
WORKDIR /app

# No Supabase values are needed here. The frontend reads its configuration at
# runtime from placeholders the Go server substitutes into index.html, which
# means the image is not tied to one Supabase project and a rotated key needs a
# restart rather than a rebuild.
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY frontend/package.json frontend/
RUN pnpm install --frozen-lockfile

COPY frontend/ frontend/
RUN pnpm --filter @scoreboard/frontend build

# ── Backend ───────────────────────────────────────────────────────────────────
FROM golang:1.25 AS api
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# ── Runtime ───────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/base-debian12
WORKDIR /app

COPY --from=api /server /app/server
COPY --from=web /app/frontend/dist /app/web

ENV STATIC_DIR=/app/web
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot

CMD ["/app/server"]
