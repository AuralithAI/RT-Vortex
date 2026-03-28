# ── RTVortex Web UI — Multi-stage Dockerfile ──────────────────────────────────
# Option A: Standalone Next.js behind nginx (docker-compose)
# Option B: Static export for CDN / S3 (NEXT_OUTPUT=export)
# ─────────────────────────────────────────────────────────────────────────────

# ── Stage 1: Dependencies ────────────────────────────────────────────────────
FROM node:20-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --legacy-peer-deps

# ── Stage 2: Build ───────────────────────────────────────────────────────────
FROM node:20-alpine AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Build argument to switch between standalone and export
ARG NEXT_OUTPUT=standalone
ENV NEXT_OUTPUT=${NEXT_OUTPUT}
ARG NEXT_PUBLIC_API_URL=""
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL}

RUN npm run build

# ── Stage 3a: Standalone server (Option A) ───────────────────────────────────
FROM node:20-alpine AS standalone
WORKDIR /app
ENV NODE_ENV=production

# Copy standalone output
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]

# ── Stage 3b: Static export (Option B) ───────────────────────────────────────
FROM nginx:1.27-alpine AS static
COPY --from=builder /app/out /usr/share/nginx/html
COPY nginx-static.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
