FROM alpine:3.14

RUN apk add --no-cache bash curl less ca-certificates git tzdata zip gettext \
    nginx curl supervisor certbot-nginx && \
    rm -rf /var/cache/apk/* && mkdir -p /run/nginx

STOPSIGNAL SIGINT
EXPOSE 80
EXPOSE 443
# docker-entrypoint.sh runs init_react_envs.sh synchronously so the
# config.json + OidcTrustedDomains.js envsubst completes BEFORE nginx
# starts serving. Then it exec's supervisord (nginx + cron + cert).
ENTRYPOINT ["/usr/local/docker-entrypoint.sh"]

WORKDIR /usr/share/nginx/html
# copy configuration files
COPY docker/default.conf /etc/nginx/http.d/default.conf
COPY docker/nginx.conf /etc/nginx/nginx.conf
COPY docker/init_cert.sh /usr/local/init_cert.sh
COPY docker/init_react_envs.sh /usr/local/init_react_envs.sh
COPY docker/docker-entrypoint.sh /usr/local/docker-entrypoint.sh
RUN chmod +x /usr/local/init_cert.sh /usr/local/init_react_envs.sh /usr/local/docker-entrypoint.sh \
    && rm /etc/crontabs/root

# configure supervisor
COPY docker/supervisord.conf /etc/supervisord.conf
# copy build files
COPY out/ /usr/share/nginx/html/