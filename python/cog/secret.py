import os
from pathlib import Path
from typing import Optional

import requests
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa

__all__ = [
    "secret",
    "secret_provider",
]

COG_ENV_LOCATION = ".cog/.env"
COG_PUBLIC_KEY_LOCATION_ENV_VAR_KEY = "COG_PUBLIC_KEY_LOCATION"


def secret(name: str) -> str:
    return secret_provider.get_secret(name)


class SecretProvider:
    def __init__(self) -> None:
        self.once = False
        self.env = {}
        self.key = rsa.generate_private_key(
            backend=default_backend(),
            public_exponent=65537,
            key_size=2048,
        )
        public_pem = self.key.public_key().public_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PublicFormat.SubjectPublicKeyInfo,
        )
        public_key_path = Path(
            os.getenv(COG_PUBLIC_KEY_LOCATION_ENV_VAR_KEY, ".cog/public_key")
        )
        public_key_path.write_bytes(public_pem)
        with open(COG_ENV_LOCATION) as f:
            for line in f:
                line = line.strip()
                kv = line.split("=", 1)
                if len(kv) != 2:
                    continue
                self.env[kv[0]] = kv[1]

    def set_secret_url(self, secret_url: Optional[str] = None) -> None:
        if self.once:
            return
        if secret_url:
            self._secret_url = secret_url
            self.once = True

    def get_secret(self, secret_name: str) -> str:
        if self._secret_url:
            response = requests.get(f"{self._secret_url}/{secret_name}")
            response.raise_for_status()

            plaintext_bytes = self.key.decrypt(
                response.content,
                padding.OAEP(
                    mgf=padding.MGF1(algorithm=hashes.SHA256()),
                    algorithm=hashes.SHA256(),
                    label=None,
                ),
            )

            return plaintext_bytes.decode("utf-8")
        else:
            return self.env.get(secret_name, "")


secret_provider = SecretProvider()
