import json

import numpy as np


DESCRIPTION_ALIASES = {
    "拖鞋": "鞋子",
    "凉鞋": "鞋子",
    "运动鞋": "鞋子",
    "皮鞋": "鞋子",
    "服装": "衣服",
    "衣物": "衣服",
    "裙装": "连衣裙",
    "轿车": "汽车",
    "单车": "自行车",
    "笔记本": "笔记本电脑",
    "显示器": "屏幕",
    "耳麦": "耳机",
}


def parse_model_output(text, allowed_labels):
    value = text.strip()
    start = value.find("{")
    end = value.rfind("}")
    if start < 0 or end <= start:
        raise ValueError("model response does not contain a JSON object")
    payload = json.loads(value[start:end + 1])
    description = str(payload.get("description", "")).strip().strip('“”"')
    raw_tags = payload.get("tags", [])
    if not isinstance(raw_tags, list):
        raise ValueError("model tags must be an array")
    candidates = []
    allowed = set(allowed_labels)
    for item in raw_tags:
        tag = str(item).strip()
        if tag in allowed and tag not in candidates:
            candidates.append(tag)
        if len(candidates) == 8:
            break
    return description, candidates


def augment_candidates_from_description(description, candidates, allowed_labels):
    result = list(dict.fromkeys(candidates))
    for label in allowed_labels:
        if len(label) >= 2 and label in description and label not in result:
            result.append(label)
    for phrase, label in DESCRIPTION_ALIASES.items():
        if phrase in description and label in allowed_labels and label not in result:
            result.append(label)
    return result


def select_validated_tags(frame_scores, candidates, label_index, media_type, min_score=0.28, max_tags=5):
    scores = np.asarray(frame_scores, dtype=np.float32)
    if scores.ndim != 2:
        raise ValueError("frame_scores must have shape [frames, labels]")
    required_hits = 1 if media_type == "image" else min(2, scores.shape[0])
    result = []
    for tag in candidates:
        index = label_index.get(tag)
        if index is None:
            continue
        values = scores[:, index]
        matched = values[values >= min_score]
        if matched.size < required_hits:
            continue
        result.append({"tag": tag, "confidence": round(float(matched.mean()), 6)})
    result.sort(key=lambda item: (-item["confidence"], item["tag"]))
    return result[:max_tags]
