import json
import re

import numpy as np


COLORS = ("红色", "蓝色", "绿色", "黄色", "橙色", "紫色", "粉色", "黑色", "白色", "灰色", "棕色", "金色", "银色")
SPECIFIC_OBJECTS = (
    "紧身衣", "牛仔裤", "连衣裙", "短裙", "长裙", "短裤", "长裤", "运动裤", "丝袜", "短袜", "长袜",
    "运动鞋", "高跟鞋", "皮鞋", "凉鞋", "拖鞋", "泳装", "运动服", "西装", "外套", "上衣", "衬衫", "毛衣",
)
INVALID_TAGS = {"未知", "无法判断", "不确定", "其他", "内容", "画面", "媒体"}
GENERIC_CLOTHING = {"衣服", "服装", "衣物", "裤子", "鞋子"}

DESCRIPTION_ALIASES = {
    "裙装": "连衣裙",
    "舞蹈": "跳舞",
    "跳舞": "跳舞",
    "轿车": "汽车",
    "单车": "自行车",
    "笔记本": "笔记本电脑",
    "显示器": "屏幕",
    "耳麦": "耳机",
}

REQUIRED_TAG_DEFAULTS = {
    "shoes": "鞋子无法判断",
    "socks": "袜子无法判断",
    "closeup": "无明显特写",
    "clothing": "服装无法判断",
    "scenes": "场景无法判断",
    "actions": "动作无法判断",
    "people": "人数无法判断",
}

SCENE_HINTS = {
    "客厅", "卧室", "厨房", "浴室", "餐厅", "阳台", "走廊", "楼梯", "室内", "室外", "办公室", "会议室",
    "车间", "仓库", "体育馆", "游泳池", "操场", "舞台", "展厅", "商场", "商店内部", "海边", "海滩",
    "街道", "公园", "车内", "教室", "酒店",
}
ACTION_HINTS = {
    "坐着", "躺着", "站立", "站着", "俯卧", "仰卧", "跳舞", "做操", "走路", "跑步", "游泳", "骑行",
    "瑜伽", "健身", "登山", "徒步", "拍摄", "阅读", "写字", "烹饪", "工作", "学习",
}


def parse_model_output(text, allowed_labels):
    description, candidates, _ = parse_model_analysis(text, allowed_labels)
    return description, candidates


def parse_model_analysis(text, allowed_labels, require_categories=False):
    value = text.strip()
    start = value.find("{")
    end = value.rfind("}")
    if start < 0 or end <= start:
        raise ValueError("model response does not contain a JSON object")
    payload = json.loads(value[start:end + 1])
    description = str(payload.get("description", "")).strip().strip('“”"')
    required_tags = _required_tags(payload.get("requiredTags"), require_categories)
    raw_tags = payload.get("tags", [])
    if not isinstance(raw_tags, list):
        raise ValueError("model tags must be an array")
    candidates = list(required_tags)
    for item in raw_tags:
        tag = str(item).strip()
        if _valid_tag(tag) and tag not in candidates:
            candidates.append(tag)
        if len(candidates) == 10:
            break
    return description, candidates, set(required_tags)


def _required_tags(value, fill_missing=False):
    if not isinstance(value, dict):
        value = {}
    normalized = {}
    keys = ("shoes", "socks", "closeup", "clothing", "scenes", "actions", "people")
    for key in keys:
        items = value.get(key, [])
        if not isinstance(items, list):
            items = [items]
        normalized[key] = [str(item).strip() for item in items if _valid_tag(str(item).strip())]
        if fill_missing and not normalized[key]:
            normalized[key] = [REQUIRED_TAG_DEFAULTS[key]]
    result = []
    # Keep one answer for every mandatory category before using the remaining
    # slots for additional garments, scenes, and actions.
    for key in keys:
        if normalized[key] and normalized[key][0] not in result:
            result.append(normalized[key][0])
    for key in ("clothing", "scenes", "actions"):
        for tag in normalized[key][1:]:
            if tag not in result:
                result.append(tag)
            if len(result) == 10:
                return result
    return result


def augment_candidates_from_description(description, candidates, allowed_labels):
    result = list(dict.fromkeys(candidates))
    for object_name in SPECIFIC_OBJECTS:
        if object_name not in description:
            continue
        compound = _color_object_phrase(description, object_name)
        value = compound or object_name
        if value not in result:
            result.append(value)
    for label in allowed_labels:
        if len(label) >= 2 and label in description and label not in result:
            result.append(label)
    for phrase, label in DESCRIPTION_ALIASES.items():
        if phrase in description and label not in result:
            result.append(label)
    return _suppress_generic_tags(result)


def reconcile_required_defaults(candidates, required_tags):
    candidates = list(candidates)
    required = set(required_tags)
    predicates = {
        "shoes": lambda tag: tag == "未穿鞋" or any(name in tag for name in ("运动鞋", "高跟鞋", "皮鞋", "凉鞋", "拖鞋")),
        "socks": lambda tag: tag == "未穿袜" or any(name in tag for name in ("短袜", "长袜", "丝袜", "袜子")),
        "closeup": lambda tag: tag.endswith("特写") and tag != "无明显特写",
        "clothing": lambda tag: any(name in tag for name in SPECIFIC_OBJECTS),
        "scenes": lambda tag: tag in SCENE_HINTS,
        "actions": lambda tag: tag in ACTION_HINTS,
        "people": lambda tag: bool(re.fullmatch(r"(?:一个人|两个人|\d+个人|\d+人)", tag)),
    }
    for key, default in REQUIRED_TAG_DEFAULTS.items():
        if default not in required:
            continue
        replacement = next((tag for tag in candidates if tag != default and predicates[key](tag)), None)
        if replacement is None:
            continue
        candidates = [tag for tag in candidates if tag != default]
        required.remove(default)
        required.add(replacement)
    return candidates[:10], required


def _valid_tag(tag):
    if tag in INVALID_TAGS or len(tag) < 1 or len(tag) > 12:
        return False
    return re.fullmatch(r"[\u3400-\u9fffA-Za-z0-9＋+#·-]+", tag) is not None


def _color_object_phrase(description, object_name):
    for color in COLORS:
        pattern = re.escape(color) + r"(?:的)?" + re.escape(object_name)
        if re.search(pattern, description):
            return color + object_name
    return ""


def _suppress_generic_tags(tags):
    unique = [tag for tag in dict.fromkeys(tags) if _valid_tag(tag)]
    compound_colors = {color for color in COLORS if any(tag.startswith(color) and len(tag) > len(color) for tag in unique)}
    specific_clothing = [tag for tag in unique if any(name in tag for name in SPECIFIC_OBJECTS)]
    result = []
    for tag in unique:
        if tag in compound_colors:
            continue
        if specific_clothing and tag in GENERIC_CLOTHING:
            continue
        if tag == "外套" and any(value.endswith("外套") and value != "外套" for value in specific_clothing):
            continue
        if tag == "人物" and specific_clothing:
            continue
        result.append(tag)
    return result[:10]


def select_validated_tags(frame_scores, candidates, label_index, media_type, min_score=0.28, max_tags=10, required_tags=()):
    scores = np.asarray(frame_scores, dtype=np.float32)
    if scores.ndim != 2:
        raise ValueError("frame_scores must have shape [frames, labels]")
    required_hits = 1 if media_type == "image" else min(2, scores.shape[0])
    required_tags = set(required_tags)
    result = []
    for tag in candidates:
        index = label_index.get(tag)
        if index is None:
            continue
        values = scores[:, index]
        matched = values[values >= min_score]
        required = tag in required_tags
        if not required and matched.size < required_hits:
            continue
        confidence_values = matched if matched.size else values
        confidence = max(0.0, min(1.0, float(confidence_values.mean())))
        result.append({"tag": tag, "confidence": round(confidence, 6), "_required": required})
    result.sort(key=lambda item: (not item["_required"], -item["confidence"], item["tag"]))
    for item in result:
        item.pop("_required", None)
    return result[:max_tags]
