# syntax=docker/dockerfile:1
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /web
COPY package.json package-lock.json ./
# Keep the scoped registry public configuration separate from private credentials.
RUN printf '@sre-norns:registry=https://npm.pkg.github.com\n' > .npmrc
# The package token is a build secret, never an image layer or build argument.
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc,required=true npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED
LABEL org.opencontainers.image.source="https://github.com/sre-norns/urth" \
      org.opencontainers.image.title="Urth website" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.created=$CREATED
ENV URTH_API_UPSTREAM=api-server:8080
# The standard entrypoint renders the upstream template before nginx starts.
RUN sed -i -e '/^user /d' -e 's@^pid .*;@pid /tmp/nginx.pid;@' /etc/nginx/nginx.conf && \
    chown -R nginx:nginx /etc/nginx/conf.d /var/cache/nginx
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /web/dist /usr/share/nginx/html
USER nginx
EXPOSE 8080
