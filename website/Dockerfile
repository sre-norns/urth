# syntax=docker/dockerfile:1
FROM node:24-alpine AS build
WORKDIR /web
COPY package.json package-lock.json .npmrc ./
# The package token is a build secret, never an image layer or build argument.
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc,required=true npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
ENV URTH_API_UPSTREAM=api-server:8080
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /web/dist /usr/share/nginx/html
EXPOSE 8080
