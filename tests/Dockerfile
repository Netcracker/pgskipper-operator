FROM ghcr.io/netcracker/qubership-docker-integration-tests:0.2.2

ENV LC_ALL=en_US.UTF-8 \
    LANG=en_US.UTF-8

COPY docker/pip.conf /root/.pip/pip.conf
COPY docker/requirements.txt /root/requirements.txt
COPY docker/uid_entrypoint /opt/uid_entrypoint

RUN set -x \
    && apk add --update --no-cache build-base postgresql-dev \
    && pip3 install --no-cache-dir -r /root/requirements.txt \
    && pip3 uninstall -y pip

RUN chmod -R g=u /etc/passwd && \
    mkdir /app && \
    mkdir /test_runs

COPY ./app/* /app/
COPY robot /test_runs/

RUN chgrp -R 0 /app && chmod g+w /app && \
    chgrp -R 0 /test_runs && chmod -R g+w /test_runs && \
    chmod +x /opt/uid_entrypoint

# Volumes are defined to support read-only root file system
VOLUME /etc
VOLUME /app
VOLUME /test_runs
VOLUME /tmp

USER 1001
WORKDIR /app

ENTRYPOINT [ "/opt/uid_entrypoint" ]
CMD ["robot -i ${TESTS_TAGS} /test_runs/"]
