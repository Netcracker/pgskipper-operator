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
import subprocess


def touch(filepath):
    open(filepath, "w").close()


# def get_folder_size(dirpath):
# 	total_size = 0
# 	for dirpath, dirname, filenames in os.walk(dirpath):
# 		for f in filenames:
# 			fp = os.path.join(dirpath, f)
# 			if os.path.exists(fp):
# 				total_size += os.path.getsize(fp)
# 	return total_size

def get_folder_size(dirpath):
    p1 = subprocess.Popen(["du", "-sb", dirpath], stdout=subprocess.PIPE)
    size = p1.communicate()[0].split(b"\t")[0]
    return size.decode()


def get_mount_point(pathname):
    """Get the mount point of the filesystem containing pathname"""
    pathname = os.path.normcase(os.path.realpath(pathname))
    parent_device = path_device = os.stat(pathname).st_dev
    while parent_device == path_device:
        mount_point = pathname
        pathname = os.path.dirname(pathname)
        if pathname == mount_point: break
        parent_device = os.stat(pathname).st_dev
    return mount_point


def get_mounted_device(pathname):
    """Get the device mounted at pathname"""
    # uses "/proc/mounts"
    pathname = os.path.normcase(pathname)  # might be unnecessary here
    try:
        with open("/proc/mounts", "r") as ifp:
            for line in ifp:
                fields = line.rstrip('\n').split()
                # note that line above assumes that
                # no mount points contain whitespace
                if fields[1] == pathname:
                    return fields[0]
    except EnvironmentError:
        pass
    return None  # explicit


def get_fs_space(pathname):
    """Get the free space and total of the filesystem containing pathname"""

    stat = os.statvfs(pathname)
    return (stat.f_bfree * stat.f_bsize, stat.f_blocks * stat.f_bsize)
