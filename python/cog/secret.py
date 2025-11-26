from __future__ import annotations

import base64
import os
from pathlib import Path

import requests
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding, rsa
from dotenv import dotenv_values

__all__ = [
    "load_secret",
    "default_secret_provider",
]


def load_secret(name: str, secret_provider: SecretProvider | None) -> str:
    if not secret_provider:
        secret_provider = default_secret_provider
    return secret_provider.get_secret(name)


class SecretProvider:
    def __init__(
        self,
        cog_env_location: str = ".cog/.env",
        cog_public_key_env_var: str = "COG_PUBLIC_KEY_LOCATION",
    ) -> None:
        self.env = {}
        self.no_public_key = False
        self.key = rsa.generate_private_key(
            backend=default_backend(),
            public_exponent=65537,
            key_size=2048,
        )
        self.secret_url: str | None = None
        public_pem = self.key.public_key().public_bytes(
            encoding=serialization.Encoding.PEM,
            format=serialization.PublicFormat.SubjectPublicKeyInfo,
        )
        public_key_path_raw = os.getenv(cog_public_key_env_var)
        if not public_key_path_raw:
            self.no_public_key = True
            return
        public_key_path = Path(public_key_path_raw)
        public_key_path.parent.mkdir(mode=0o700, exist_ok=True)
        public_key_path.touch()
        public_key_path.write_bytes(public_pem)
        if not os.path.isfile(cog_env_location):
            return
        self.env = dotenv_values(cog_env_location)

    def get_secret(self, secret_name: str) -> str:
        # Try to get the secret from the remote. Fall back to the local
        # env file (local development only)
        try:
            if not self.secret_url:
                raise ValueError("No secret URL passed")
            if self.no_public_key:
                raise ValueError("No public key for encryption")
            raw_secret = os.getenv(secret_name)
            if not raw_secret:
                raise ValueError("No matching secret")
            response = requests.post(
                f"{self.secret_url}",
                json={
                    "value": raw_secret,
                },
            )
            response.raise_for_status()

            plaintext_bytes = self.key.decrypt(
                base64.b64decode(response.text),
                padding.OAEP(
                    mgf=padding.MGF1(algorithm=hashes.SHA256()),
                    algorithm=hashes.SHA256(),
                    label=None,
                ),
            )

            return plaintext_bytes.decode("utf-8")
        except Exception:
            return self.env.get(secret_name) or ""


default_secret_provider = SecretProvider()
