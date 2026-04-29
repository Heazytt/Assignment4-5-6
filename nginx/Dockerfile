FROM nginx:1.27-alpine

COPY nginx/nginx.conf       /etc/nginx/nginx.conf
COPY nginx/proxy_params.conf /etc/nginx/proxy_params.conf
COPY frontend/               /usr/share/nginx/html/

EXPOSE 80

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost/ >/dev/null || exit 1
