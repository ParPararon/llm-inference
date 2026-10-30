#!/usr/bin/env python3

import json
import statistics
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path


URL = "http://127.0.0.1:8080/v1/chat/completions"
MODEL = "Qwen/Qwen2.5-1.5B-Instruct"
RUNS = 5
TIMEOUT = 180

TOKEN_PATH = Path("secrets/api-token")
OUTPUT_PATH = Path("docs/benchmark-results.json")

SHORT_PROMPT = "Explain what a database index is in two sentences."

LONG_PROMPT = (
    "Consider the following data engineering system. "
    "An application collects events from web clients, mobile clients, and backend services. "
    "Events are delivered through a message broker and processed by several independent consumers. "
    "Each event contains a timestamp, event identifier, user identifier, event type, and metadata. "
    "Some events arrive late, some are duplicated, and some are delivered out of order. "
    "The system must support real-time analytics and daily batch reporting. "
    "Storage costs, fault tolerance, schema evolution, and reproducible processing are important. "
    "The engineering team also needs monitoring, alerting, and a strategy for replaying historical data. "
    "Data quality checks should detect missing fields, unexpected values, and broken relationships. "
    "Access to sensitive fields must be restricted, and retention policies must be enforced. "
) * 12 + (
    "Describe the main components of this architecture and explain "
    "how you would handle duplicate events, late arrivals, and data quality."
)


def request(token, prompt):
    payload = {
        "model": MODEL,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 64,
        "temperature": 0,
        "stream": True,
        "stream_options": {"include_usage": True},
    }

    req = urllib.request.Request(
        URL,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
        method="POST",
    )

    started = time.perf_counter()
    first_token = None
    completion_tokens = None
    prompt_tokens = None
    done = False

    with urllib.request.urlopen(req, timeout=TIMEOUT) as response:
        if response.status != 200:
            raise RuntimeError(f"Unexpected HTTP status: {response.status}")

        for raw_line in response:
            line = raw_line.decode("utf-8").strip()

            if not line.startswith("data: "):
                continue

            data = line[6:]

            if data == "[DONE]":
                done = True
                break

            chunk = json.loads(data)

            usage = chunk.get("usage")
            if usage:
                prompt_tokens = usage.get("prompt_tokens")
                completion_tokens = usage.get("completion_tokens")

            for choice in chunk.get("choices", []):
                content = choice.get("delta", {}).get("content")

                if content and first_token is None:
                    first_token = time.perf_counter()

    finished = time.perf_counter()

    if not done:
        raise RuntimeError("Stream ended without [DONE]")

    if first_token is None:
        raise RuntimeError("No generated text received")

    latency = finished - started
    ttft = first_token - started

    # Decode throughput excludes the time spent waiting for the first token.
    decode_time = latency - ttft
    throughput = None

    if completion_tokens is not None and completion_tokens > 1 and decode_time > 0:
        throughput = (completion_tokens - 1) / decode_time

    return {
        "ttft_s": round(ttft, 4),
        "latency_s": round(latency, 4),
        "prompt_tokens": prompt_tokens,
        "completion_tokens": completion_tokens,
        "decode_tokens_per_s": round(throughput, 2) if throughput is not None else None,
    }


def median(values):
    values = [value for value in values if value is not None]
    return round(statistics.median(values), 4) if values else None


def main():
    token = TOKEN_PATH.read_text(encoding="utf-8").strip()

    if not token:
        raise RuntimeError("API token is empty")

    cases = {
        "short": SHORT_PROMPT,
        "long": LONG_PROMPT,
    }

    results = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "model": MODEL,
        "runs_per_case": RUNS,
        "streaming": True,
        "max_tokens": 64,
        "cases": {},
    }

    for name, prompt in cases.items():
        print(f"\n=== {name} prompt ===", flush=True)
        print("Warmup...", flush=True)

        request(token, prompt)

        samples = []

        for index in range(1, RUNS + 1):
            sample = request(token, prompt)
            samples.append(sample)

            print(
                f"Run {index}/{RUNS}: "
                f"TTFT={sample['ttft_s']:.3f}s, "
                f"latency={sample['latency_s']:.3f}s, "
                f"tokens={sample['completion_tokens']}, "
                f"decode={sample['decode_tokens_per_s']} tok/s",
                flush=True,
            )

        summary = {
            "median_ttft_s": median([s["ttft_s"] for s in samples]),
            "median_latency_s": median([s["latency_s"] for s in samples]),
            "median_prompt_tokens": median([s["prompt_tokens"] for s in samples]),
            "median_completion_tokens": median([s["completion_tokens"] for s in samples]),
            "median_decode_tokens_per_s": median(
                [s["decode_tokens_per_s"] for s in samples]
            ),
        }

        results["cases"][name] = {
            "prompt": prompt,
            "samples": samples,
            "summary": summary,
        }

        print("Median:", json.dumps(summary, ensure_ascii=False), flush=True)

    OUTPUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT_PATH.write_text(
        json.dumps(results, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )

    print(f"\nResults saved to {OUTPUT_PATH}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, urllib.error.URLError) as error:
        print(f"Benchmark failed: {error}", file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("\nBenchmark interrupted", file=sys.stderr)
        sys.exit(130)
