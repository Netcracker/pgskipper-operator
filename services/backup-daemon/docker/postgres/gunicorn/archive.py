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

from flask import Flask
from flask_restful import Api

import configs
import endpoints.wal
import storage


app = Flask('ArchiveEndpoints')
api = Api(app)

conf = configs.load_configs()
storage_instance = storage.init_storage(storageRoot=conf['storage'])

api.add_resource(endpoints.wal.Upload, *endpoints.wal.Upload.get_endpoints(), resource_class_args=(storage_instance, ))
api.add_resource(endpoints.wal.Download, *endpoints.wal.Download.get_endpoints(), resource_class_args=(storage_instance, ))
api.add_resource(endpoints.wal.Delete, *endpoints.wal.Delete.get_endpoints(), resource_class_args=(storage_instance, ))


if __name__ == '__main__':
    app.run()
