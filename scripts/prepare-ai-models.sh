#!/bin/sh
set -eu
ROOT="${1:-./data/app/ai-models}"
mkdir -p "$ROOT/chinese-clip/onnx"
fetch() {
  rel="$1" size="$2" sha="$3" url="$4" target="$ROOT/$1"
  if [ -f "$target" ] && [ "$(wc -c < "$target" | tr -d ' ')" = "$size" ] && echo "$sha  $target" | sha256sum -c - >/dev/null 2>&1; then return; fi
  mkdir -p "$(dirname "$target")"
  rm -f "$target.part"
  curl --fail --location --retry 5 --retry-all-errors --continue-at - --output "$target.part" "$url"
  [ "$(wc -c < "$target.part" | tr -d ' ')" = "$size" ]
  echo "$sha  $target.part" | sha256sum -c -
  mv "$target.part" "$target"
}
fetch Qwen3VL-2B-Instruct-Q4_K_M.gguf 1107409952 089d75c52f4b7ffc56ba998ffc50aae89fcafc755f9e7208aacca281dca6c2ae 'https://huggingface.co/Qwen/Qwen3-VL-2B-Instruct-GGUF/resolve/52d6c8ffea26cc873ac5ad116f8631268d7eb503/Qwen3VL-2B-Instruct-Q4_K_M.gguf'
fetch mmproj-Qwen3VL-2B-Instruct-Q8_0.gguf 445053216 f9a68fabba69c3b81e153367b2c7521030b0fa8bb0de400c9599c8e6725f9c82 'https://huggingface.co/Qwen/Qwen3-VL-2B-Instruct-GGUF/resolve/52d6c8ffea26cc873ac5ad116f8631268d7eb503/mmproj-Qwen3VL-2B-Instruct-Q8_0.gguf'
fetch chinese-clip/onnx/model.onnx 753665706 d4e282affd5f09e196856cc63fbd0e77c576f598fdf6f6bb78ee61f1ef7cd770 'https://huggingface.co/Xenova/chinese-clip-vit-base-patch16/resolve/f26904860903e70e050b8f48255e5f48401816e9/onnx/model.onnx'
BASE='https://huggingface.co/Xenova/chinese-clip-vit-base-patch16/resolve/f26904860903e70e050b8f48255e5f48401816e9'
for rel in config.json preprocessor_config.json tokenizer.json tokenizer_config.json special_tokens_map.json vocab.txt; do
  target="$ROOT/chinese-clip/$rel"
  [ -s "$target" ] || curl --fail --location --retry 5 --output "$target.part" "$BASE/$rel" && { [ -s "$target" ] || mv "$target.part" "$target"; }
done
echo "AI models ready: $ROOT"
