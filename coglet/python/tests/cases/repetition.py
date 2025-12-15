from typing import List, Optional

from cog import BaseModel, BasePredictor, Input


class Output(BaseModel):
    rs: str
    os: Optional[str]
    # List[T] not allowed for outputs, CSV instead
    ls: str
    rd0: str
    # rd1: str  # Invalid default=None
    rd2: str
    od0: Optional[str]
    od1: Optional[str]
    od2: Optional[str]
    # List[T] not allowed for outputs, CSV instead
    ld0: str
    # ld1: str
    ld2: str


FIXTURE = [
    (
        {
            'rs': 'foo0',
            'ls': ['bar0', 'baz0'],
            'rd0': 'foo1',
            # 'rd1': 'foo2',  # Invalid default=None
            'ld0': ['bar1', 'baz1'],
            # 'ld1': ['bar2', 'baz2'],  # Invalid default=None
        },
        Output(
            rs='foo0',
            os=None,
            ls='bar0,baz0',
            rd0='foo1',
            # rd1='foo2',  # Invalid default=None
            rd2='foo',
            od0=None,
            od1=None,
            od2='bar',
            ld0='bar1,baz1',
            # ld1='bar2,baz2',  # Invalid default=None
            ld2='',
        ),
    ),
    (
        {
            'rs': 'foo0',
            'os': 'foo1',
            'ls': ['bar0', 'baz0'],
            'rd0': 'foo1',
            # 'rd1': 'foo2',  # Invalid default=None
            'rd2': 'foo3',
            'od0': 'foo4',
            'od1': 'foo5',
            'od2': 'foo6',
            'ld0': ['bar1', 'baz1'],
            # 'ld1': ['bar2', 'baz2'],  # Invalid default=None
            'ld2': ['bar3', 'baz3'],
        },
        Output(
            rs='foo0',
            os='foo1',
            ls='bar0,baz0',
            rd0='foo1',
            # rd1='foo2',  # Invalid default=None
            rd2='foo3',
            od0='foo4',
            od1='foo5',
            od2='foo6',
            ld0='bar1,baz1',
            # ld1='bar2,baz2',  # Invalid default=None
            ld2='bar3,baz3',
        ),
    ),
]


class Predictor(BasePredictor):
    test_inputs = {
        'rs': 'foo',
    }
    setup_done = False

    def setup(self) -> None:
        self.setup_done = True

    def predict(
        self,
        rs: str,
        os: Optional[str],
        ls: List[str],
        rd0: str = Input(),
        # rd1: str = Input(default=None),  # Invalid default=None
        rd2: str = Input(default='foo'),
        od0: Optional[str] = Input(),
        od1: Optional[str] = Input(default=None),
        od2: Optional[str] = Input(default='bar'),
        ld0: List[str] = Input(),
        # ld1: List[str] = Input(default=None),  # Invalid default=None
        ld2: List[str] = Input(default_factory=list),
    ) -> Output:
        return Output(
            rs=rs,
            os=os,
            ls=','.join(ls),
            rd0=rd0,
            # rd1=rd1,  # Invalid default=None
            rd2=rd2,
            od0=od0,
            od1=od1,
            od2=od2,
            ld0=','.join(ld0),
            # ld1=','.join(ld1),  # Invalid default=None
            ld2=','.join(ld2),
        )
