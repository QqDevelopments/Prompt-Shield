"""Exercise pinned SDKs against the test-owned gateway and synthetic provider."""

import os

from anthropic import Anthropic
from openai import OpenAI

base = os.environ["SDK_BASE_URL"]
key = os.environ["SDK_API_KEY"]
text = "Contact alice@example.com"
messages = [{"role": "user", "content": text}]
with OpenAI(base_url=base + "/v1", api_key=key, max_retries=0) as client:
    result = client.chat.completions.create(model="fixture-model", messages=messages)
    assert result.choices[0].message.content == text
    chunks = client.chat.completions.create(
        model="fixture-model", messages=messages, stream=True
    )
    assert "".join(chunk.choices[0].delta.content or "" for chunk in chunks) == text
    result = client.embeddings.create(
        model="fixture-model", input=[text], encoding_format="float"
    )
    assert result.data[0].embedding == [0.25, 0.75]
with OpenAI(
    base_url=base + "/v1/gemini",
    api_key=key,
    max_retries=0,
    default_headers={"X-Provider-API-Key": "synthetic-gemini-sdk-key"},
) as client:
    result = client.chat.completions.create(model="fixture-model", messages=messages)
    assert result.choices[0].message.content == text
    chunks = client.chat.completions.create(
        model="fixture-model", messages=messages, stream=True
    )
    assert "".join(chunk.choices[0].delta.content or "" for chunk in chunks) == text
with Anthropic(base_url=base, api_key=key, max_retries=0) as client:
    result = client.messages.create(
        model="fixture-model", max_tokens=128, messages=messages
    )
    assert result.content[0].text == text
    with client.messages.stream(
        model="fixture-model", max_tokens=128, messages=messages
    ) as stream:
        assert "".join(stream.text_stream) == text
print(
    "SDK compatibility: OpenAI chat/stream/embeddings, Gemini chat/stream and Anthropic messages/stream passed"
)
