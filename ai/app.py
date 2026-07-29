import base64
import io
import json
import logging
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
from tag_hierarchy import attach_tag_hierarchy
from tag_logic import limit_closeup_candidates, parse_structured_model_analysis, reconcile_closeups_from_description, select_validated_tags

MODEL_DIR = Path("/models/chinese-clip")
MEDIA_ROOT = Path("/Media").resolve()
CACHE_ROOT = Path("/cache").resolve()
TAG_MODEL = "Qwen3-VL-candidates+Chinese-CLIP-validation"
TAG_MODEL_VERSION = "Qwen-f982a075+Xenova-f2690486-ai-classified-v6"
DESCRIPTION_MODEL = "Qwen3-VL-8B-Instruct-Q4_K_M"
DESCRIPTION_MODEL_VERSION = "Qwen-f982a07559d4a2f6c8744d840bf6fccab30eea96"
LOCK = threading.Lock()
CONTROL_LOCK = threading.Lock()
PAUSED = threading.Event()
LLAMA_PROCESS = None
LLAMA_LOG = None
ACTIVE_MEDIA_PROCESSES = set()
ANALYSIS_EPOCH = 0
LOGGER = logging.getLogger("lpicto.ai")

ANALYSIS_JSON_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["description", "tags"],
    "properties": {
        "description": {"type": "string"},
        "tags": {
            "type": "array",
            "maxItems": 10,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": ["label", "category", "subject", "type", "color", "style"],
                "properties": {
                    "label": {"type": "string"},
                    "category": {"type": "string", "enum": ["people", "action", "clothing", "closeup"]},
                    "subject": {"type": "string", "enum": ["count", "posture", "activity", "shoes", "socks", "top", "outerwear", "dress", "pants", "sportswear", "swimwear", "hat", "accessories", "part"]},
                    "type": {"type": "string"},
                    "color": {"type": "string", "enum": ["", "黑", "白", "灰", "红", "橙", "黄", "绿", "青", "蓝", "紫", "粉", "棕"]},
                    "style": {"type": "string"}
                }
            }
        },
    },
}

LLAMA_COMMAND = [
    "llama-server", "-m", "/models/Qwen3VL-8B-Instruct-Q4_K_M.gguf",
    "--mmproj", "/models/mmproj-Qwen3VL-8B-Instruct-Q8_0.gguf",
    "--host", "127.0.0.1", "--port", "8091", "-c", "8192", "-t", "8",
    "-ngl", "0", "--parallel", "1", "--image-min-tokens", "256", "--image-max-tokens", "512",
]

class AnalyzeRequest(BaseModel):
    assetId: int
    relPath: str
    mediaType: str
    cacheKey: str
    duration: float | None = None
    focus: str = ""
    stagedPath: str = ""


class ModelOutputInvalidError(ValueError):
    def __init__(self, attempts):
        super().__init__("模型输出格式错误，已自动重新生成 1 次")
        self.attempts = attempts


def _retry_analysis_prompt(prompt):
    return (
        "这些画面请重新独立分析，识别范围、判断标准和输出含义保持不变。"
        "这一次先逐项确定结构化标签的字段和值，最后再填写 description；仍只输出符合 schema 的 JSON。\n"
        + prompt.replace("请分析", "请再次分析", 1)
    )


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

def _safe_stage_path(rel_path):
    target = (CACHE_ROOT / rel_path.replace("\\", "/")).resolve()
    stage_root = (CACHE_ROOT / "ai-staging").resolve()
    if target != stage_root and stage_root not in target.parents:
        raise ValueError("stage path escapes cache root")
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
    if request.stagedPath:
        stage_path = _safe_stage_path(request.stagedPath)
        meta_path = stage_path / "meta.json"
        if not stage_path.is_dir() or not meta_path.is_file():
            raise FileNotFoundError(stage_path)
        meta = json.loads(meta_path.read_text(encoding="utf-8"))
        ratios = meta.get("ratios") or [0.0]
        frame_paths = sorted(stage_path.glob("*.jpg"))
        if not frame_paths:
            raise FileNotFoundError(f"no staged frames in {stage_path}")
        frames = []
        for index, frame_path in enumerate(frame_paths):
            with Image.open(frame_path) as image:
                ratio = float(ratios[index]) if index < len(ratios) else 0.0
                frames.append((ratio, image.convert("RGB").copy()))
        return frames
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
    focus_instruction = ""
    if focus.strip():
        focus_instruction = f"\n重点识别：{focus.strip()}。画面中可见时，优先写入描述并选择对应标签。"
    prompt = f"""请分析{"按时间顺序抽取的视频画面" if media_type == "video" else "图片"}，只识别人物、动作、鞋子、袜子、衣服和特写，输出描述和最多 10 个结构化标签。不要识别或描述背景、场景、地点、物体、自然、交通、食物、天气和媒体形式。每个标签必须由你直接判断 category 与 subject，禁止依赖标签文字推断分类。
category/subject 允许组合：
people→count；action→posture/activity；
clothing→shoes/socks/top/outerwear/dress/pants/sportswear/swimwear/hat/accessories；
closeup→part。
label 是用于浏览的完整中文标签；type 是具体类型或地点，color 只能从黑、白、灰、红、橙、黄、绿、青、蓝、紫、粉、棕中选择一个单字，style 是款式或图案，不适用的属性写空字符串。例如：
白色波点纱裙={{label:"白色波点纱裙",category:"clothing",subject:"dress",type:"纱裙",color:"白",style:"波点"}}
红色手绳={{label:"红色手绳",category:"clothing",subject:"accessories",type:"手绳",color:"红",style:""}}
勾脚={{label:"勾脚",category:"action",subject:"activity",type:"勾脚",color:"",style:""}}
固定标准：人数的 label 和 type 必须使用阿拉伯数字“N人”，例如 1人、2人、3人，禁止“一人、一个人、两个人”；姿态统一使用坐姿、躺姿、站立、蹲姿、跪姿、俯卧、仰卧；走路统一为行走、舞蹈统一为跳舞、体操统一为做操。特写只允许脸部、头部、眼部、鼻部、嘴部、嘴唇、舌部、牙齿、耳部、颈部、肩部、锁骨、胸部、腹部、肚脐、腰部、背部、手部、手掌、手指、手臂、肘部、手腕、臀部、腿部、大腿、膝部、小腿、脚踝、脚部、脚底、脚趾、全身，并输出“部位＋特写”；命中具体部位时禁止再输出范围更大的重复特写，图片只输出最主要的一个特写，视频最多输出两个不同特写。裤袜、连裤袜、紧身裤袜、丝袜及其他带“袜”的穿着必须使用 clothing→socks，禁止归入 pants；紧身裤仍使用 pants。圆点图案统一写波点，格子统一写格纹。
颜色忽略深浅：米白、乳白、米色归白，银归灰，金归黄，褐色和卡其色归棕；“浅色、深色、亮色、暗色”没有明确色相时 color 留空。label 中可写“白色连裤袜”等完整名称，但 color 必须只写单字。标签优先覆盖鞋袜、全部可见衣物、明确特写、动作和人数。people 只允许人数，不生成“女子、女孩、男性”等人物类型；身体部位只有构成明显特写时才生成特写标签。颜色必须绑定具体对象，禁止单独输出颜色；无法确认的项目不生成标签，任何包含“无法判断”的内容只能写入 description。description 使用 40 到 120 个简体中文字符，只描述人物数量、动作、鞋袜、衣服和特写，不写背景及其他内容；视频标签以抽帧中清晰可见的内容为准。{focus_instruction}"""
    content = [{"type": "text", "text": prompt}] + [{"type": "image_url", "image_url": {"url": _data_url(image)}} for _, image in frames]
    payload = {
        "model": DESCRIPTION_MODEL,
        "messages": [{"role": "user", "content": content}],
        "temperature": 0.1,
        "max_tokens": 768,
        "stream": False,
        "response_format": {
            "type": "json_schema",
            "json_schema": {"name": "media_analysis", "strict": True, "schema": ANALYSIS_JSON_SCHEMA},
        },
    }
    parsed = None
    failed_attempts = []
    for attempt in range(2):
        _check_analysis(epoch)
        if attempt:
            payload["temperature"] = 0.0
            payload["messages"][0]["content"][0]["text"] = _retry_analysis_prompt(prompt)
        response = httpx.post("http://127.0.0.1:8091/v1/chat/completions", json=payload, timeout=600)
        response.raise_for_status()
        _check_analysis(epoch)
        body = response.json()
        choice = body["choices"][0]
        raw = choice["message"]["content"]
        try:
            parsed = parse_structured_model_analysis(raw)
            break
        except (ValueError, json.JSONDecodeError) as exc:
            diagnostic_output = raw[:2048] + ("…" if len(raw) > 2048 else "")
            failed_attempts.append(
                {
                    "attempt": attempt + 1,
                    "parseError": str(exc),
                    "finishReason": choice.get("finish_reason", ""),
                    "output": diagnostic_output,
                }
            )
            LOGGER.warning(
                "invalid model JSON attempt=%d finish_reason=%s response_length=%d error=%s response=%r",
                attempt + 1,
                choice.get("finish_reason", ""),
                len(raw),
                exc,
                diagnostic_output,
            )
    if parsed is None:
        raise ModelOutputInvalidError(failed_attempts)
    description, candidates, required_tags, model_metadata = parsed
    candidates, model_metadata = reconcile_closeups_from_description(description, candidates, model_metadata)
    candidates, model_metadata = limit_closeup_candidates(candidates, model_metadata, media_type)
    required_tags = set(candidates)
    description = "".join(description.splitlines()).strip()
    if len(description) < 40:
        description = description.rstrip("。") + "，并记录画面中可见的人物数量、穿着、动作与特写细节。"
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
    return description[:120], attach_tag_hierarchy(tags, model_metadata)

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
    except ModelOutputInvalidError as exc:
        raise HTTPException(
            500,
            {
                "code": "model_output_invalid",
                "message": str(exc),
                "attempts": exc.attempts,
            },
        ) from exc
    except Exception as exc:
        if PAUSED.is_set() or epoch != ANALYSIS_EPOCH:
            raise HTTPException(409, "AI analysis paused for media playback") from exc
        raise HTTPException(500, str(exc)) from exc
