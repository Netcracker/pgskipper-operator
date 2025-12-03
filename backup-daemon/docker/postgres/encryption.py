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

from Crypto.Cipher import AES
import os
import io
import json
import logging
from kubernetes.client.rest import ApiException


class FileWrapper(object):
    def __init__(self, file_path, encrypted):
        self.__log = logging.getLogger("EncryptionHelper")
        self.__file_path = file_path
        self.__encrypted = encrypted
        self.chunk_size = 4096
        if os.path.exists(file_path):
            self.__file_size = os.path.getsize(file_path)

    def get_file_stream(self):
        if self.__encrypted:
            return self.get_decrypted()
        else:
            return io.FileIO(self.__file_path, "r", closefd=True)

    def put_file_stream(self, stream):
        """
        This method saves file stream to a actual file on FS
        in case of encryption, saves encrypted file and creates .key file with
        metadata about encryption key. Used only for WAL archives
        :param stream: file stream that should be saved
        :return: sha256 - sha256 of processed file
        """
        import hashlib
        sha256 = hashlib.sha256()
        if self.__encrypted:
            password = KeyManagement.get_object().get_password()
            cipher = EncryptionHelper.get_cipher_by_pw(password)
        with io.FileIO(self.__file_path, "w", closefd=True) as target:
            data = stream.read(self.chunk_size)
            while True:
                next_data = stream.read(self.chunk_size)
                sha256.update(data)
                if self.__encrypted:
                    # pad = False
                    # if len(data) % AES.block_size != 0:
                    #     pad = True
                    pad = len(next_data) == 0   # try to pad only last chunk
                    data = cipher.encrypt(data, pad)
                    self.create_key_file()
                target.write(data)
                if len(next_data) == 0:
                    stream.close()
                    self.__log.info("Processed stream with sha256 {}".format(
                        sha256.hexdigest()))
                    return sha256.hexdigest()
                else:
                    data = next_data

    def get_decrypted(self):
        """
        in case of encryption everything is pretty simple,
        need to decrypt file by chunks and then return it as a stream
        :return: decrypted file stream
        """
        self.__log.info("Will try to decrypt file: %s" % self.__file_path)
        cipher = EncryptionHelper.get_cipher_by_pw(
            self.get_password_for_file())
        import tempfile
        decrypted = tempfile.TemporaryFile(mode='w+')
        with io.FileIO(self.__file_path, "r", closefd=True) as encrypted:
            number_of_chunks = self.__file_size / self.chunk_size
            iteration = 0
            while True:
                data = encrypted.read(self.chunk_size)
                if len(data) == 0:
                    encrypted.close()
                    # return stream to the start
                    decrypted.seek(0)
                    return decrypted
                data = cipher.decrypt(data)
                if iteration == number_of_chunks:
                    data = cipher.unpad(data)
                iteration = iteration + 1
                decrypted.write(data)

    def get_password_for_file(self):
        """
        this method gets password by file name.
        in case of WAL file metadata about the key is saved to .key file,
        in case of full backup in .metrics.
        :return: encryption key as a string
        """
        if self.__file_path.endswith("tar.gz"):  # full backup
            key_filename = os.path.dirname(self.__file_path) + "/.metrics"
        else:  # WAL archive
            key_filename = self.__file_path + ".key"

        if os.path.exists(key_filename):
            with open(key_filename) as f:
                data = json.load(f)
            key_name, key_source = data["key_name"], data["key_source"]
            self.__log.info("Will use key_name: {} and key_source: {} "
                            "for file decryption: {}".
                            format(key_name, key_source, self.__file_path))
            return KeyManagement(key_source,
                                 key_name).get_password()
        else:
            # if file not exists for some of the reason, return default PW
            return KeyManagement.get_object().get_password()

    def create_key_file(self):
        key_info = {
            "key_name": KeyManagement.get_key_name(),
            "key_source": KeyManagement.get_key_source()
        }
        key_filename = self.__file_path + ".key"
        with open(key_filename, 'w+') as outfile:
            json.dump(key_info, outfile)


class EncryptionHelper(object):
    def __init__(self, key_management):
        self.log = logging.getLogger("EncryptionHelper")
        self.__key_management = key_management

    @staticmethod
    def evp_bytes_to_key(password, key_len=32, iv_len=16):
        """
        Derive the key and the IV from the given password and salt.
        """
        from hashlib import md5
        d_tot = md5(password).digest()
        d = [d_tot]
        while len(d_tot) < (iv_len + key_len):
            d.append(md5(d[-1] + password).digest())
            d_tot += d[-1]
        return d_tot[:key_len], d_tot[key_len:key_len + iv_len]

    @staticmethod
    def get_cipher_by_pw(password):
        key, iv = EncryptionHelper.evp_bytes_to_key(password)
        return CipherWrapper(iv, key)


class KeyManagement(object):
    def __init__(self, pw_source, pw_name):
        self.log = logging.getLogger("KeyManagement")
        self.pw_source = pw_source
        self.pw_name = pw_name
        # self.pw_version = pw_version

    @staticmethod
    def get_object():
        pw_source = os.getenv("KEY_SOURCE", 'kubernetes')
        pw_name = os.getenv("KEY_NAME", "daemon-secret")
        return KeyManagement(pw_source, pw_name)

    def get_password(self):
        if self.pw_source.lower() == KeySources.KUBERNETES:
            pw_name = os.getenv("KEY_NAME", "daemon-secret")
            return KubernetesPassword(pw_name).get_password()
        elif self.pw_source.lower() == KeySources.VAULT:
            pw_name = os.getenv("KEY_NAME", "daemon-secret")
            return VaultPassword(pw_name).get_password()

    def get_password_by_name(self, key_name):
        self.log.info("Try to get key: {}".format(key_name))
        if self.pw_source.lower() == KeySources.KUBERNETES:
            return KubernetesPassword(key_name).get_password()
        elif self.pw_source.lower() == KeySources.VAULT:
            return VaultPassword(key_name).get_password()

    @staticmethod
    def get_key_name():
        return os.getenv("KEY_NAME", "daemon-secret").lower()

    @staticmethod
    def get_key_source():
        return os.getenv("KEY_SOURCE", "kubernetes").lower()


# here should be some abstract class
class KubernetesPassword(object):
    SA_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

    def __init__(self, key_name):
        self.log = logging.getLogger("KubernetesPassword")
        self.key_name = key_name

    def get_password(self):
        from kubernetes import config
        from kubernetes.client.apis import core_v1_api
        config.load_incluster_config()
        api = core_v1_api.CoreV1Api()
        # https://github.com/kubernetes-client/python/issues/363
        namespace = open(self.SA_PATH).read()
        try:
            api_response = api.read_namespaced_secret(self.key_name, namespace)
            import base64
            return base64.b64decode(api_response.data.get("password"))
        except ApiException as exc:
            self.log.error(exc)
            raise exc


class VaultPassword(object):
    def __init__(self, key_name):
        pass

    def get_password(self):
        pass


class CipherWrapper(object):
    def __init__(self, iv, key):
        from Crypto.Cipher import AES
        self.__cipher = AES.new(key, AES.MODE_CBC, iv)

    def encrypt(self, data, pad=False):
        return self.__cipher.encrypt(self.pad(data) if pad else data)

    def decrypt(self, data, unpad=False):
        return self.unpad(self.__cipher.decrypt(data)) \
            if unpad else self.__cipher.decrypt(data)

    def pad(self, data):
        from Crypto.Util.Padding import pad
        return pad(data, AES.block_size)

    def unpad(self, data):
        from Crypto.Util.Padding import unpad
        return unpad(data, AES.block_size)


class KeySources:
    KUBERNETES = "kubernetes"
    VAULT = "vault"

    def __init__(self):
        pass
