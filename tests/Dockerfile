# Copyright 2024-2025 NetCracker Technology Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
