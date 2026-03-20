ARG REGISTRY_VERSION=3
FROM registry:${REGISTRY_VERSION}

RUN apk add --no-cache apache2-utils

COPY config.yml /etc/docker/registry/config.yml
COPY config-auth.yml /etc/docker/registry/config-auth.yml
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 5000

ENTRYPOINT ["/entrypoint.sh"]
