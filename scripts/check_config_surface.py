#!/usr/bin/env python3
"""
Fail when the configuration surface drifts between code, .env.example,
deployment artifact, and docker-compose.
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

def extract_config_read_keys() -> set[str]:
    """Keys read by config.Load via getEnv/getDuration/getInt/getFloat/getOptionalPositiveInt."""
    src = (ROOT / "internal/platform/config/config.go").read_text()
    # Matches getEnv("LIKI_...") and similar helpers
    pattern = re.compile(r'get(?:Env|Duration|Int|Float|OptionalPositiveInt)\("([A-Z_]+)"')
    return set(pattern.findall(src))

def extract_example_keys() -> set[str]:
    """Keys declared in .env.example (lines starting with LIKI_*=)."""
    txt = (ROOT / ".env.example").read_text()
    return set(re.findall(r"^(LIKI_[A-Z_]+)=", txt, re.MULTILINE))

def extract_referenced_keys() -> set[str]:
    """Keys referenced by deployment artifact and docker-compose.yml (env values, endpointEnv, tokenEnv, build args)."""
    dep = (ROOT / "dev/agent-deployment/deployment.json").read_text()
    comp = (ROOT / "dev/docker-compose.yml").read_text()
    combined = dep + "\n" + comp
    # Matches LIKI_<NAME> anywhere (in endpointEnv/tokenEnv strings, env: values, build args)
    return set(re.findall(r"LIKI_[A-Z_]+", combined))

def main() -> int:
    read_keys = extract_config_read_keys()
    example_keys = extract_example_keys()
    referenced_keys = extract_referenced_keys()

    # A. Every key read by config must be documented in .env.example
    missing_from_example = read_keys - example_keys
    # B. Every key in .env.example must be either read by config or referenced by deployment/compose
    # (Keys only in .env.example with no consumer are dead documentation)
    dead_in_example = example_keys - read_keys - referenced_keys

    errors = []
    if missing_from_example:
        errors.append("config reads keys missing from .env.example: " + ", ".join(sorted(missing_from_example)))
    if dead_in_example:
        # Known acceptable: LIKI_LLM_MAX_OUTPUT_TOKENS is in example, read by config, ok
        # Filter out known acceptable? None expected.
        errors.append(".env.example keys consumed nowhere (read nor referenced): " + ", ".join(sorted(dead_in_example)))

    if errors:
        print("Configuration surface drift detected:\n  - " + "\n  - ".join(errors), file=sys.stderr)
        return 1

    print("Configuration surface OK:")
    print(f"  config reads: {len(read_keys)} keys")
    print(f"  .env.example: {len(example_keys)} keys")
    print(f"  referenced by deployment/compose: {len(referenced_keys)} keys")
    return 0

if __name__ == "__main__":
    sys.exit(main())
