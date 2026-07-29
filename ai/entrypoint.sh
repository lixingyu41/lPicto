#!/bin/sh
set -eu
MODEL=/models/Qwen3VL-8B-Instruct-Q4_K_M.gguf
MMPROJ=/models/mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf
CLIP=/models/chinese-clip/onnx/model.onnx
for file in "$MODEL" "$MMPROJ" "$CLIP"; do
  test -s "$file" || { echo "required model missing: $file" >&2; exit 42; }
done
exec uvicorn app:app --host 0.0.0.0 --port 8090 --workers 1
