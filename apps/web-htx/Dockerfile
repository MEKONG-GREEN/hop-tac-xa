# Web App HTX (MEKONG GREEN) — build nhiều tầng, runtime tối thiểu, chạy không phải root.
# Build context: apps/web-htx
#   docker build -t agrigreen/web-htx \
#     --build-arg NEXT_PUBLIC_API_BASE_URL=https://demo-htx.mekonggreen.vn/api \
#     --build-arg NEXT_PUBLIC_ADMIN_API_BASE=https://demo-htx.mekonggreen.vn/admin-api \
#     --build-arg NEXT_PUBLIC_OIDC_ISSUER=https://demo-htx.mekonggreen.vn/auth/realms/agrigreen \
#     apps/web-htx
# Biến NEXT_PUBLIC_* được nhúng lúc build → mỗi môi trường một image.

FROM node:24-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# --ignore-scripts: không chạy postinstall của gói phụ thuộc trong image.
RUN npm ci --ignore-scripts --no-audit --no-fund

FROM node:24-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
# Không có mặc định localhost: bản production (NEXT_PUBLIC_DEPLOY_ENV mặc định) bắt buộc truyền đủ
# URL https, thiếu là `next build` dừng (src/deployEnv.ts). Compose cục bộ: NEXT_PUBLIC_DEPLOY_ENV=dev.
# Giả lập (NEXT_PUBLIC_API_MOCK=1) chỉ build được với NEXT_PUBLIC_DEPLOY_ENV=demo|dev.
ARG NEXT_PUBLIC_DEPLOY_ENV=production
ARG NEXT_PUBLIC_API_BASE_URL=
ARG NEXT_PUBLIC_ADMIN_API_BASE=
ARG NEXT_PUBLIC_OIDC_ISSUER=
ARG NEXT_PUBLIC_OIDC_CLIENT_ID=agrigreen-web-ops
ARG NEXT_PUBLIC_MAP_STYLE_URL=
ARG NEXT_PUBLIC_SUPPORT_NAME=
ARG NEXT_PUBLIC_SUPPORT_PHONE=
ARG NEXT_PUBLIC_SUPPORT_EMAIL=
ARG NEXT_PUBLIC_API_MOCK=0
ENV NEXT_PUBLIC_DEPLOY_ENV=$NEXT_PUBLIC_DEPLOY_ENV \
    NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL \
    NEXT_PUBLIC_ADMIN_API_BASE=$NEXT_PUBLIC_ADMIN_API_BASE \
    NEXT_PUBLIC_OIDC_ISSUER=$NEXT_PUBLIC_OIDC_ISSUER \
    NEXT_PUBLIC_OIDC_CLIENT_ID=$NEXT_PUBLIC_OIDC_CLIENT_ID \
    NEXT_PUBLIC_MAP_STYLE_URL=$NEXT_PUBLIC_MAP_STYLE_URL \
    NEXT_PUBLIC_SUPPORT_NAME=$NEXT_PUBLIC_SUPPORT_NAME \
    NEXT_PUBLIC_SUPPORT_PHONE=$NEXT_PUBLIC_SUPPORT_PHONE \
    NEXT_PUBLIC_SUPPORT_EMAIL=$NEXT_PUBLIC_SUPPORT_EMAIL \
    NEXT_PUBLIC_API_MOCK=$NEXT_PUBLIC_API_MOCK
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# prebuild chép worker MapLibre vào public/maplibre rồi next build (output: standalone).
RUN npm run build

FROM node:24-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public
USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:3000/login || exit 1
CMD ["node", "server.js"]
