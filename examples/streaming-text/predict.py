from threading import Thread
from typing import Iterator

import torch
from transformers import AutoModelForCausalLM, AutoTokenizer, TextIteratorStreamer

from cog import BaseRunner, Input, streaming

MODEL_NAME = "HuggingFaceTB/SmolLM2-135M-Instruct"


class Predictor(BaseRunner):
    def setup(self) -> None:
        self.device = "cuda" if torch.cuda.is_available() else "cpu"
        dtype = torch.float16 if self.device == "cuda" else torch.float32

        self.tokenizer = AutoTokenizer.from_pretrained(MODEL_NAME)
        self.model = AutoModelForCausalLM.from_pretrained(
            MODEL_NAME,
            torch_dtype=dtype,
        ).to(self.device)
        self.model.eval()

    @streaming
    def run(
        self,
        prompt: str = Input(description="Prompt to complete"),
        max_new_tokens: int = Input(
            description="Maximum number of tokens to generate",
            default=128,
            ge=1,
            le=512,
        ),
    ) -> Iterator[str]:
        messages = [{"role": "user", "content": prompt}]
        text = self.tokenizer.apply_chat_template(
            messages,
            tokenize=False,
            add_generation_prompt=True,
        )
        inputs = self.tokenizer([text], return_tensors="pt").to(self.device)
        streamer = TextIteratorStreamer(
            self.tokenizer,
            skip_prompt=True,
            skip_special_tokens=True,
        )

        generation_kwargs = {
            **inputs,
            "streamer": streamer,
            "max_new_tokens": max_new_tokens,
            "do_sample": True,
            "temperature": 0.7,
            "top_p": 0.9,
            "pad_token_id": self.tokenizer.eos_token_id,
        }

        thread = Thread(target=self.model.generate, kwargs=generation_kwargs)
        thread.start()

        try:
            for chunk in streamer:
                if chunk:
                    yield chunk
        finally:
            thread.join()
