FROM ghcr.io/netcracker/qubership-docker-integration-tests:main

ENV LC_ALL=en_US.UTF-8 \
    LANG=en_US.UTF-8

# Base image may run as non-root; install as root so /root and site-packages are writable
USER root

COPY docker/pip.conf /root/.pip/pip.conf
COPY docker/requirements.txt /root/requirements.txt

# Base image defines Alpine release; pinning apk packages here would duplicate index and break on parent bumps.
# hadolint ignore=DL3018
RUN set -x \
    && apk add --update --no-cache build-base postgresql-dev \
    && pip3 install --no-cache-dir -r /root/requirements.txt \
    && pip3 uninstall -y pip

RUN chmod -R g=u /etc/passwd && \
    mkdir /app && \
    mkdir /test_runs

# Project structure (unchanged): app and robot under /app, /test_runs
COPY ./app/* /app/
COPY robot /test_runs/
# Also expose tests under base image layout so new scripts (adapter-S3, etc.) can run
COPY robot ${ROBOT_HOME}/tests

COPY docker/uid_entrypoint /opt/uid_entrypoint
# Wrapper: set TAGS from TESTS_TAGS then run base entrypoint (adapter-S3, run-robot)
COPY docker/pgskipper-robot-entrypoint.sh /opt/pgskipper-robot-entrypoint.sh

RUN chgrp -R 0 /app && chmod g+w /app && \
    chgrp -R 0 /test_runs && chmod -R g+w /test_runs && \
    chmod +x /opt/uid_entrypoint /opt/pgskipper-robot-entrypoint.sh && \
    chown -R 1000:0 "${ROBOT_HOME}" && chmod -R 775 "${ROBOT_HOME}"

# Volumes as in original (read-only root FS support, etc.)
VOLUME /etc
VOLUME /app
VOLUME /test_runs
VOLUME /tmp

USER 1001
WORKDIR /app

ENTRYPOINT ["/opt/uid_entrypoint"]
# Use new scripts flow (base entrypoint + adapter-S3); tags from TESTS_TAGS env
CMD ["/opt/pgskipper-robot-entrypoint.sh", "run-robot"]
