# hello

A simple pipeline that transforms your text input by converting it to uppercase and prefixing it with "HELLO".

https://replicate.com/pipelines-beta/hello

## Features

- Converts any text input to uppercase
- Adds a friendly "HELLO" prefix to your text
- Simple, single-input interface

## Models

Under the hood it uses these models:

- [pipelines-beta/upcase](https://replicate.com/pipelines-beta/upcase): A utility model that converts text to uppercase

## How it works

The pipeline takes a text prompt as input, passes it to the `upcase` model to convert the text to uppercase, and then adds "HELLO" as a prefix to the transformed text. This creates a greeting-style output from any input text.
Edit model
