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
        self.no_public_key = False
        self.key = rsa.generate_private_key(
            backend=default_backend(),
            public_exponent=65537,
            key_size=2048,
        )
        public_pem = self.key.public_key().public_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PublicFormat.SubjectPublicKeyInfo,
        )
        public_key_path_raw = os.getenv(COG_PUBLIC_KEY_LOCATION_ENV_VAR_KEY)
        if not public_key_path_raw:
            self.no_public_key = True
            return
        public_key_path = Path(public_key_path_raw)
        os.makedirs(os.path.basename(public_key_path), exist_ok=True)
        public_key_path.touch()
        public_key_path.write_bytes(public_pem)
        if not os.path.isfile(COG_ENV_LOCATION):
            return
        with open(COG_ENV_LOCATION) as f:
            for line in f:
                line = line.strip()
                kv = line.split("=", 1)
                # Skip bad entries with no equals sign
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
        # Try to get the secret from the remote. Fall back to the local
        # env file (local development only)
        try:
            if not self._secret_url:
                raise ValueError("No secret URL passed")
            if self.no_public_key:
                raise ValueError("No public key for encryption")
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
        except Exception:
            return self.env.get(secret_name, "")


secret_provider = SecretProvider()
