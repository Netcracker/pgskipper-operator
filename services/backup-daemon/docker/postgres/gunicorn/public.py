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

from flask_restful import Api
import os
from flask import Flask

import configs
import endpoints.backup
import endpoints.restore
import endpoints.status
import storage

from opentelemetry.instrumentation.flask import FlaskInstrumentor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource


app = Flask("PublicEndpoints")
collector_endpoint = os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
if collector_endpoint != "":
    collector_endpoint = "http://" + collector_endpoint
    NAMESPACE_PATH = '/var/run/secrets/kubernetes.io/serviceaccount/namespace'
    ns = open(NAMESPACE_PATH).read()
    resource = Resource(attributes={
        SERVICE_NAME: "postgresql-backup-daemon-" + ns
    })
    provider = TracerProvider(resource=resource)
    processor = BatchSpanProcessor(OTLPSpanExporter(endpoint=collector_endpoint, insecure=True))
    provider.add_span_processor(processor)
    FlaskInstrumentor().instrument_app(app=app, tracer_provider=provider, excluded_urls="health,/health,v2/health,/v2/health")
api = Api(app)

conf = configs.load_configs()
storage_instance = storage.init_storage(storageRoot=conf['storage'])

endpoints.restore.ExternalRestoreRequest.cleanup_restore_status(storage_instance)

api.add_resource(endpoints.status.Status, *endpoints.status.Status.get_endpoints(), resource_class_args=(storage_instance,))
api.add_resource(endpoints.backup.BackupRequest, *endpoints.backup.BackupRequest.get_endpoints())
api.add_resource(endpoints.status.Health, *endpoints.status.Health.get_endpoints(), resource_class_args=(storage_instance,))
api.add_resource(endpoints.status.BackupStatus, *endpoints.status.BackupStatus.get_endpoints(), resource_class_args=(storage_instance,))
api.add_resource(endpoints.status.ExternalRestoreStatus, *endpoints.status.ExternalRestoreStatus.get_endpoints(), resource_class_args=(storage_instance,))
api.add_resource(endpoints.restore.ExternalRestoreRequest, *endpoints.restore.ExternalRestoreRequest.get_endpoints(), resource_class_args=(storage_instance,))

if __name__ == '__main__':
    app.run()
