import json
from mitmproxy import http
import time
import uuid
import logging

class OpenAIMock:
    """
    A simple mitmproxy addon that returns a mock chat completion response 
    for all requests to api.openai.com
    """
    
    def request(self, flow: http.HTTPFlow) -> None:
        logging.info("New REQUEST ------------------------- from host - " + str(flow.request.pretty_host))

        # Check if the request is going to OpenAI's API
        if "api.openai.com" in flow.request.pretty_host:
            # Create a mock response immediately without sending the request to server
            mock_response = {
                "id": f"chatcmpl-{uuid.uuid4().hex}",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": "gpt-4-mocked",
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": "This is a mocked response from the OpenAI API. Your request was intercepted by mitmproxy."
                        },
                        "finish_reason": "stop"
                    }
                ],
                "usage": {
                    "prompt_tokens": 10,
                    "completion_tokens": 20,
                    "total_tokens": 30
                }
            }
            
            # Replace the response with our mock
            flow.response = http.Response.make(
                200,  # Status code
                json.dumps(mock_response).encode(),  # Response body
                {"Content-Type": "application/json"}  # Headers
            )

addons = [OpenAIMock()]
# To use this addon with mitmproxy:
# 1. Save this script as openai_mock.py
# 2. Run: mitmdump -s openai_mock.py
# 3. Configure your application to use the mitmproxy as HTTP/HTTPS proxy