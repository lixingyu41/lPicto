import base64
import io
import os
import subprocess
import tempfile
import threading
import time
from pathlib import Path

import httpx
import numpy as np
import onnxruntime as ort
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from PIL import Image, ImageOps
from transformers import ChineseCLIPProcessor

from taxonomy import LABELS, TAXONOMY_VERSION
from media_sampling import sample_ratios
from tag_logic import augment_candidates_from_description, parse_model_analysis, reconcile_required_defaults, select_validated_tags

MODEL_DIR = Path("/models/chinese-clip")
MEDIA_ROOT = Path("/Media").resolve()
CACHE_ROOT = Path("/cache").resolve()
TAG_MODEL = "Qwen3-VL-candidates+Chinese-CLIP-validation"
TAG_MODEL_VERSION = "Qwen-52d6c8ff+Xenova-f2690486-compound-v4"
DESCRIPTION_MODEL = "Qwen3-VL-2B-Instruct-Q4_K_M"
DESCRIPTION_MODEL_VERSION = "Qwen-52d6c8ffea26cc873ac5ad116f8631268d7eb503"
LOCK = threading.Lock()
CONTROL_LOCK = threading.Lock()
PAUSED = threading.Event()
LLAMA_PROCESS = None
LLAMA_LOG = None
ACTIVE_MEDIA_PROCESSES = set()
ANALYSIS_EPOCH = 0

LLAMA_COMMAND = [
    "llama-server", "-m", "/models/Qwen3VL-2B-Instruct-Q4_K_M.gguf",
    "--mmproj", "/models/mmproj-Qwen3VL-2B-Instruct-Q8_0.gguf",
    "--host", "127.0.0.1", "--port", "8091", "-c", "4096", "-t", "6",
    "-ngl", "0", "--parallel", "1", "--image-min-tokens", "256", "--image-max-tokens", "512",
]

class AnalyzeRequest(BaseModel):
    assetId: int
    relPath: str
    mediaType: str
    cacheKey: str
    duration: float | None = None
    focus: str = ""

app = FastAPI(title="lPicto local AI", docs_url=None, redoc_url=None)
processor = ChineseCLIPProcessor.from_pretrained(MODEL_DIR, local_files_only=True)
session_options = ort.SessionOptions()
session_options.enable_cpu_mem_arena = False
session_options.enable_mem_pattern = False
session = ort.InferenceSession(
    str(MODEL_DIR / "onnx/model.onnx"),
    sess_options=session_options,
    providers=["CPUExecutionProvider"],
)
input_names = {item.name for item in session.get_inputs()}
output_names = [item.name for item in session.get_outputs()]

def _onnx_inputs(batch):
    return {key: np.asarray(value) for key, value in batch.items() if key in input_names}

def _output(outputs, wanted):
    for name, value in zip(output_names, outputs):
        if wanted in name.lower():
            return value
    raise RuntimeError(f"ONNX output {wanted!r} missing: {output_names}")

def _text_embeddings(labels):
    dummy = Image.new("RGB", (224, 224), "gray")
    vectors = []
    for start in range(0, len(labels), 100):
        prompts = [f"一张包含{label}的照片" for label in labels[start:start + 100]]
        batch = processor(text=prompts, images=dummy, return_tensors="np", padding=True)
        outputs = session.run(None, _onnx_inputs(batch))
        vectors.append(_output(outputs, "text_embeds"))
    value = np.concatenate(vectors, axis=0).astype(np.float32)
    return value / np.maximum(np.linalg.norm(value, axis=1, keepdims=True), 1e-12)

def _start_llama():
    global LLAMA_PROCESS, LLAMA_LOG
    with CONTROL_LOCK:
        if PAUSED.is_set() or (LLAMA_PROCESS is not None and LLAMA_PROCESS.poll() is None):
            return
        LLAMA_LOG = open("/tmp/llama-server.log", "ab", buffering=0)
        LLAMA_PROCESS = subprocess.Popen(LLAMA_COMMAND, stdout=LLAMA_LOG, stderr=subprocess.STDOUT)

def _stop_llama():
    global LLAMA_PROCESS, LLAMA_LOG
    with CONTROL_LOCK:
        process = LLAMA_PROCESS
        LLAMA_PROCESS = None
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=2)
        if LLAMA_LOG is not None:
            LLAMA_LOG.close()
            LLAMA_LOG = None

def _check_analysis(epoch):
    if PAUSED.is_set() or epoch != ANALYSIS_EPOCH:
        raise RuntimeError("AI analysis paused for media playback")

@app.on_event("startup")
def start_models():
    _start_llama()

@app.on_event("shutdown")
def stop_models():
    _stop_llama()

def _safe_media_path(rel_path):
    target = (MEDIA_ROOT / rel_path.replace("\\", "/")).resolve()
    if MEDIA_ROOT not in target.parents:
        raise ValueError("media path escapes root")
    return target

def _video_frames(path, duration):
    ratios = sample_ratios(duration)
    frames = []
    with tempfile.TemporaryDirectory(prefix="lpicto-ai-") as temp:
        for index, ratio in enumerate(ratios):
            out = Path(temp) / f"{index}.jpg"
            timestamp = max(0.0, float(duration or 0) * ratio)
            command = ["ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-ss", f"{timestamp:.3f}", "-i", str(path), "-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-q:v", "3", "-y", str(out)]
            process = subprocess.Popen(command)
            with CONTROL_LOCK:
                ACTIVE_MEDIA_PROCESSES.add(process)
                paused = PAUSED.is_set()
            try:
                if paused:
                    process.terminate()
                return_code = process.wait(timeout=120)
                if return_code != 0:
                    raise subprocess.CalledProcessError(return_code, command)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=2)
                raise
            finally:
                with CONTROL_LOCK:
                    ACTIVE_MEDIA_PROCESSES.discard(process)
            with Image.open(out) as image:
                frames.append((ratio, image.convert("RGB").copy()))
    return frames

def _load_frames(request):
    media_path = _safe_media_path(request.relPath)
    if not media_path.is_file():
        raise FileNotFoundError(media_path)
    # NFS can keep directory entries and stat data available after the backing
    # server disconnects. Force one real read before starting image decoding or
    # ffmpeg so the API can pause the whole source instead of failing every item.
    with media_path.open("rb") as source:
        source.read(1)
    if request.mediaType == "video":
        return _video_frames(media_path, request.duration)
    preview = CACHE_ROOT / "previews" / f"{request.cacheKey}.webp"
    source = preview if preview.is_file() else media_path
    with Image.open(source) as image:
        return [(0.0, ImageOps.exif_transpose(image).convert("RGB"))]

def _image_embedding(image):
    batch = processor(text=["照片"], images=image, return_tensors="np", padding=True)
    outputs = session.run(None, _onnx_inputs(batch))
    value = _output(outputs, "image_embeds").astype(np.float32)
    return value / np.maximum(np.linalg.norm(value, axis=1, keepdims=True), 1e-12)

def _frame_scores(frames, labels):
    vectors = np.concatenate([_image_embedding(image) for _, image in frames], axis=0)
    return vectors @ _text_embeddings(labels).T

def _data_url(image):
    copy = image.copy(); copy.thumbnail((1280, 1280))
    output = io.BytesIO(); copy.save(output, "JPEG", quality=85, optimize=True)
    return "data:image/jpeg;base64," + base64.b64encode(output.getvalue()).decode("ascii")

def _analysis(frames, media_type, epoch, focus=""):
    structured_focus = "必须输出以下7类标签" in focus
    focus_instruction = ""
    if focus.strip():
        focus_instruction = f"\n重点识别：{focus.strip()}。画面中可见时，优先写入描述并选择对应标签。"
    prompt = f"""请分析图片中可见的主体、场景和动作，并严格输出一个 JSON 对象：
{{"description":"40到120个简体中文字符的客观描述","tags":["标签1","标签2"]}}
输出 3 到 10 个能直接用于分类的具体中文短语，每个 2 到 12 个字符。标签应优先表达“属性＋具体对象”和具体动作，例如“紫色紧身衣”“牛仔裤”“跳舞”。
颜色修饰物体时必须与物体组成一个标签，不要单独输出“紫色”；能够识别具体服装时不要输出“衣服”“裤子”“人物”等泛化标签；使用“牛仔裤”而不是“裤子”，使用“跳舞”而不是“舞蹈”。
禁止推测人物身份、精确地点或画面外事件，不要输出 JSON 之外的文字。{focus_instruction}"""
    if media_type == "video":
        prompt = f"""以下图片按视频时间顺序抽取。请概括多帧共同可见的主体、场景和动作变化，并严格输出一个 JSON 对象：
{{"description":"40到120个简体中文字符的客观描述","tags":["标签1","标签2"]}}
输出 3 到 10 个至少在两帧中共同出现、能直接用于分类的具体中文短语，每个 2 到 12 个字符。标签应优先表达“属性＋具体对象”和具体动作，例如“紫色紧身衣”“牛仔裤”“跳舞”。
颜色修饰物体时必须与物体组成一个标签，不要单独输出“紫色”；能够识别具体服装时不要输出“衣服”“裤子”“人物”等泛化标签；使用“牛仔裤”而不是“裤子”，使用“跳舞”而不是“舞蹈”。
禁止推测人物身份、精确地点或未显示事件，不要输出 JSON 之外的文字。{focus_instruction}"""
    if structured_focus:
        prompt = f"""请分析{"按时间顺序抽取的视频画面" if media_type == "video" else "图片"}，严格输出以下 JSON，不要省略 requiredTags 的任何字段：
{{"description":"40到120个简体中文字符的客观描述","requiredTags":{{"shoes":"鞋类标签","socks":"袜子标签","closeup":"特写标签","clothing":["服装标签"],"scenes":["场景标签"],"actions":["动作标签"],"people":"人数标签"}},"tags":[]}}
标签规则：
1. shoes：清楚未穿鞋写“未穿鞋”；穿鞋写具体类型，能看清颜色时写成“白色运动鞋”；脚部不可见写“鞋子无法判断”。
2. socks：清楚未穿袜写“未穿袜”；穿袜必须把颜色和类型合成一个词，如“白色短袜”“黑色丝袜”；不可见写“袜子无法判断”。
3. closeup：写具体部位加“特写”，如“脸部特写”“腿部特写”“脚部特写”；没有明显特写写“无明显特写”。
4. clothing：逐件列出全部清晰可见的衣物，上衣、内搭、外套、裙装、裤装、鞋帽等识别到几件就写几件，不得只保留其中一件；每件都将主要颜色和具体类型合成一个词，如“紫色上衣”“黑色外套”“蓝色牛仔裤”；颜色或类型看不清时写“服装无法判断”。
5. scenes：每个明确场景各写一个词，如“室内”“舞台”“海边”；无法辨认写“场景无法判断”。
6. actions：写可见姿态或动作，如“坐着”“躺着”“站立”“跳舞”“做操”；多个明确动作可分别写，无法辨认写“动作无法判断”。
7. people：写主要画面中的可见人数：“一个人”“两个人”“3个人”“4个人”，依此类推；无法数清写“人数无法判断”。
description 必须写清人数、全部可见衣物、鞋袜、特写部位、动作、场景和有助于搜索的背景细节，使用完整自然语句，不得只重复标签。
“未穿鞋”“未穿袜”只能在脚部清楚可见时使用；禁止用“人物”“衣服”“鞋子”“裤子”这类泛词代替具体标签。标签总数最多 10 个；视频以多数抽帧共同可见的状态为准。{focus_instruction}"""
    content = [{"type": "text", "text": prompt}] + [{"type": "image_url", "image_url": {"url": _data_url(image)}} for _, image in frames]
    payload = {"model": DESCRIPTION_MODEL, "messages": [{"role": "user", "content": content}], "temperature": 0.1, "max_tokens": 360, "stream": False}
    _check_analysis(epoch)
    response = httpx.post("http://127.0.0.1:8091/v1/chat/completions", json=payload, timeout=600)
    response.raise_for_status()
    _check_analysis(epoch)
    raw = response.json()["choices"][0]["message"]["content"]
    description, candidates, required_tags = parse_model_analysis(raw, LABELS, require_categories=structured_focus)
    description = "".join(description.splitlines()).strip()
    if len(description) < 40:
        description = description.rstrip("。") + "，并记录画面中可见的人物穿着、姿态、场景与背景细节。"
    candidates = augment_candidates_from_description(description, candidates, LABELS)
    candidates, required_tags = reconcile_required_defaults(candidates, required_tags)
    if not candidates:
        return description[:120], []
    label_index = {label: index for index, label in enumerate(candidates)}
    tags = select_validated_tags(
        _frame_scores(frames, candidates),
        candidates,
        label_index,
        media_type,
        min_score=0.24,
        max_tags=10,
        required_tags=required_tags,
    )
    return description[:120], tags

def _palette(frames):
    samples = []
    for _, image in frames:
        copy = image.convert("RGB")
        copy.thumbnail((180, 180))
        pixels = np.asarray(copy).reshape(-1, 3)
        if len(pixels) > 8000:
            step = max(1, len(pixels) // 8000)
            pixels = pixels[::step]
        samples.append(pixels)
    if not samples:
        return []
    pixels = np.concatenate(samples, axis=0).astype(np.uint8)
    strip = Image.fromarray(pixels.reshape(1, len(pixels), 3), "RGB")
    quantized = strip.quantize(colors=5, method=Image.Quantize.MEDIANCUT)
    colors = quantized.getcolors(maxcolors=256) or []
    palette = quantized.getpalette() or []
    total = max(1, sum(count for count, _ in colors))
    result = []
    for count, index in sorted(colors, reverse=True)[:5]:
        offset = index * 3
        rgb = palette[offset:offset + 3]
        if len(rgb) != 3:
            continue
        result.append({"hex": "#{:02X}{:02X}{:02X}".format(*rgb), "weight": round(count / total, 4)})
    return result

@app.get("/health")
def health():
    if PAUSED.is_set():
        return {"status": "paused", "taxonomyCount": len(LABELS), "taxonomyVersion": TAXONOMY_VERSION}
    try:
        response = httpx.get("http://127.0.0.1:8091/health", timeout=2)
        response.raise_for_status()
    except Exception as exc:
        raise HTTPException(503, f"description model unavailable: {exc}") from exc
    return {"status": "ok", "taxonomyCount": len(LABELS), "taxonomyVersion": TAXONOMY_VERSION}

@app.post("/pause", status_code=202)
def pause():
    global ANALYSIS_EPOCH
    PAUSED.set()
    ANALYSIS_EPOCH += 1
    with CONTROL_LOCK:
        media_processes = list(ACTIVE_MEDIA_PROCESSES)
    for process in media_processes:
        if process.poll() is None:
            process.terminate()
    _stop_llama()
    return {"accepted": True, "status": "paused"}

@app.post("/resume", status_code=202)
def resume():
    PAUSED.clear()
    _start_llama()
    return {"accepted": True, "status": "starting"}

@app.post("/restart", status_code=202)
def restart():
    def exit_service():
        time.sleep(0.25)
        os._exit(75)
    threading.Thread(target=exit_service, daemon=True).start()
    return {"accepted": True}

@app.post("/analyze")
def analyze(request: AnalyzeRequest):
    if request.mediaType not in ("image", "video"):
        raise HTTPException(400, "unsupported media type")
    try:
        epoch = ANALYSIS_EPOCH
        _check_analysis(epoch)
        with LOCK:
            _check_analysis(epoch)
            frames = _load_frames(request)
            _check_analysis(epoch)
            description, tags = _analysis(frames, request.mediaType, epoch, request.focus)
            palette = _palette(frames)
        return {"description": description, "tags": tags, "palette": palette, "tagModel": TAG_MODEL, "tagModelVersion": TAG_MODEL_VERSION, "descriptionModel": DESCRIPTION_MODEL, "descriptionModelVersion": DESCRIPTION_MODEL_VERSION, "taxonomyVersion": TAXONOMY_VERSION, "sampledFrames": [{"ratio": ratio} for ratio, _ in frames]}
    except HTTPException:
        raise
    except Exception as exc:
        if PAUSED.is_set() or epoch != ANALYSIS_EPOCH:
            raise HTTPException(409, "AI analysis paused for media playback") from exc
        raise HTTPException(500, str(exc)) from exc
