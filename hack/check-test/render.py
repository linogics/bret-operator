#!/usr/bin/env python3
"""Run the check renderer. The pod logic lives in internal/controller, see hack/check-test/render/main.go.

Same arguments as the Go command. The check path is made absolute, because go runs from the repo root.
"""
import os
import subprocess
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
args = sys.argv[1:]
if args:
    args[0] = os.path.abspath(args[0])

sys.exit(subprocess.call(["go", "run", "./hack/check-test/render", *args], cwd=ROOT))
