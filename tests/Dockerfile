FROM ghcr.io/netcracker/qubership-docker-integration-tests:0.4.3

ENV LC_ALL=en_US.UTF-8 \
    LANG=en_US.UTF-8

USER root

COPY docker/pip.conf /root/.pip/pip.conf
COPY docker/requirements.txt /root/requirements.txt

RUN set -x \
    && apk add --update --no-cache build-base postgresql-dev \
    && pip3 install --no-cache-dir -r /root/requirements.txt \
    && pip3 uninstall -y pip

RUN chmod -R g=u /etc/passwd && \
    mkdir /app && \
    ln -s "${ROBOT_HOME}/tests" /test_runs

COPY ./app/* /app/
COPY robot ${ROBOT_HOME}/tests
COPY docker/uid_entrypoint /opt/uid_entrypoint
COPY docker/robot-entrypoint.sh /opt/robot-entrypoint.sh

RUN chgrp -R 0 /app && chmod g+w /app && \
    chmod +x /opt/uid_entrypoint /opt/robot-entrypoint.sh && \
    chown -R 1000:0 "${ROBOT_HOME}" && chmod -R 775 "${ROBOT_HOME}"

# Volumes are defined to support read-only root file system
VOLUME /etc
VOLUME /app
VOLUME /tmp

USER 1001
WORKDIR /app

ENTRYPOINT [ "/opt/uid_entrypoint" ]
CMD ["/opt/robot-entrypoint.sh", "run-robot-without-ttyd"]
