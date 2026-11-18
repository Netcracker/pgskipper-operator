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

import os
from flask import Flask

app = Flask(__name__)


# index page  (report.html)
@app.route("/")
def index():
    return main_page()


# so, here <name> is actual file name,after robot run
# it produces log, report html files
@app.route("/<name>")
def main_page(name=None):
    if not name:
        name = "report.html"

    file_name = "/app/{}".format(name)

    if os.path.exists(file_name):
        data = open(file_name, "r").read()
    else:
        data = "Can not find report or log file after robot run."

    return data


if __name__ == "__main__":
    app.run(debug=True, host="0.0.0.0")
