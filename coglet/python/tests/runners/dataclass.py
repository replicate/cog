import tempfile
from dataclasses import dataclass

from cog import BaseModel, BasePredictor, Path, Secret
from cog.coder import dataclass_coder  # noqa: F401


@dataclass(frozen=True)
class Address:
    street: str
    zip: int


@dataclass(frozen=True)
class Credentials:
    password: Secret
    pubkey: Path


@dataclass(frozen=True)
class Account:
    id: int
    name: str
    address: Address
    credentials: Credentials


class Output(BaseModel):
    account: Account


class Predictor(BasePredictor):
    test_inputs = {
        'account': Account(
            id=0,
            name='John',
            address=Address(street='Smith', zip=12345),
            credentials=Credentials(password=Secret('foo'), pubkey=Path('/etc/hosts')),
        )
    }

    def predict(self, account: Account) -> Output:
        assert type(account) is Account
        assert type(account.credentials.pubkey) is Path
        assert type(account.credentials.password) is Secret
        with open(account.credentials.pubkey, 'r') as f:
            key = f.read()
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            f.write(f'*{key}*')
        return Output(
            account=Account(
                id=account.id + 100,
                name=account.name.upper(),
                address=Address(
                    street=account.address.street.upper(),
                    zip=account.address.zip + 10000,
                ),
                credentials=Credentials(
                    password=Secret(
                        f'*{account.credentials.password.get_secret_value()}*'
                    ),
                    pubkey=Path(f.name),
                ),
            )
        )
