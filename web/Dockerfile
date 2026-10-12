FROM node:22-alpine AS build
WORKDIR /src
COPY package*.json ./
RUN npm ci
COPY . .
ARG VITE_WS_URL=ws://localhost:8080/ws
ENV VITE_WS_URL=$VITE_WS_URL
RUN npm run build

FROM nginx:alpine
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80