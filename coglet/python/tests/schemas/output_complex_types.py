from dataclasses import dataclass
from typing import List, Optional

from cog import BaseModel, BasePredictor


@dataclass
class Item:
    name: str
    value: int


@dataclass
class NestedItem:
    item: Item
    description: str


@dataclass
class Container:
    items: List[Item]
    tags: List[str]
    nested: NestedItem
    optional_list: Optional[List[str]]
    count: int


class Output(BaseModel):
    # List of primitives
    strings: List[str]
    numbers: List[int]

    # Custom class
    single_item: Item

    # List of custom classes
    items: List[Item]

    # Custom class with list fields
    container: Container

    # Nested custom classes
    nested_items: List[NestedItem]


class Predictor(BasePredictor):
    def setup(self) -> None:
        pass

    def predict(self, s: str) -> List[Output]:
        item1 = Item(name='item1', value=42)
        item2 = Item(name='item2', value=84)
        nested_item = NestedItem(item=item1, description='nested description')
        container = Container(
            items=[item1, item2],
            tags=['tag1', 'tag2'],
            nested=nested_item,
            optional_list=['opt1', 'opt2'],
            count=2,
        )

        output1 = Output(
            strings=['hello', 'world'],
            numbers=[1, 2, 3],
            single_item=item1,
            items=[item1, item2],
            container=container,
            nested_items=[nested_item],
        )

        output2 = Output(
            strings=['foo', 'bar'],
            numbers=[4, 5, 6],
            single_item=item2,
            items=[item2],
            container=container,
            nested_items=[nested_item],
        )

        return [output1, output2]


FIXTURE = [
    (
        {'s': 'test'},
        [
            Output(
                strings=['hello', 'world'],
                numbers=[1, 2, 3],
                single_item=Item(name='item1', value=42),
                items=[Item(name='item1', value=42), Item(name='item2', value=84)],
                container=Container(
                    items=[Item(name='item1', value=42), Item(name='item2', value=84)],
                    tags=['tag1', 'tag2'],
                    nested=NestedItem(
                        item=Item(name='item1', value=42),
                        description='nested description',
                    ),
                    optional_list=['opt1', 'opt2'],
                    count=2,
                ),
                nested_items=[
                    NestedItem(
                        item=Item(name='item1', value=42),
                        description='nested description',
                    )
                ],
            ),
            Output(
                strings=['foo', 'bar'],
                numbers=[4, 5, 6],
                single_item=Item(name='item2', value=84),
                items=[Item(name='item2', value=84)],
                container=Container(
                    items=[Item(name='item1', value=42), Item(name='item2', value=84)],
                    tags=['tag1', 'tag2'],
                    nested=NestedItem(
                        item=Item(name='item1', value=42),
                        description='nested description',
                    ),
                    optional_list=['opt1', 'opt2'],
                    count=2,
                ),
                nested_items=[
                    NestedItem(
                        item=Item(name='item1', value=42),
                        description='nested description',
                    )
                ],
            ),
        ],
    ),
]
