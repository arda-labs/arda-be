FROM node:22-alpine AS build

WORKDIR /src/apps/ai-runtime
COPY apps/ai-runtime/package.json apps/ai-runtime/package-lock.json ./
RUN npm ci

COPY apps/ai-runtime/tsconfig.json ./
COPY apps/ai-runtime/src ./src
RUN npm run build

FROM node:22-alpine

WORKDIR /app
COPY apps/ai-runtime/package.json apps/ai-runtime/package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY --from=build /src/apps/ai-runtime/dist ./dist

USER node
EXPOSE 8080
CMD ["node", "dist/index.js"]
