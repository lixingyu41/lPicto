import json
import re

import numpy as np

from tag_hierarchy import metadata_from_model_tag

COLORS = ("黑", "白", "灰", "红", "橙", "黄", "绿", "青", "蓝", "紫", "粉", "棕")
BODY_PART_TAGS = {
    "脸": "脸部", "脸部": "脸部", "面部": "脸部",
    "头": "头部", "头部": "头部", "头发": "头部",
    "眼": "眼部", "眼部": "眼部", "眼睛": "眼部", "双眼": "眼部",
    "鼻": "鼻部", "鼻部": "鼻部", "鼻子": "鼻部",
    "嘴": "嘴部", "嘴部": "嘴部", "口部": "嘴部", "口腔": "嘴部",
    "嘴唇": "嘴唇", "唇部": "嘴唇",
    "舌": "舌部", "舌部": "舌部", "舌头": "舌部",
    "牙": "牙齿", "牙齿": "牙齿",
    "耳": "耳部", "耳部": "耳部", "耳朵": "耳部",
    "颈": "颈部", "颈部": "颈部", "脖子": "颈部",
    "肩": "肩部", "肩部": "肩部", "肩膀": "肩部",
    "锁骨": "锁骨",
    "胸": "胸部", "胸部": "胸部",
    "腹": "腹部", "腹部": "腹部", "肚子": "腹部", "腰腹": "腹部",
    "肚脐": "肚脐",
    "腰": "腰部", "腰部": "腰部",
    "背": "背部", "背部": "背部",
    "手": "手部", "手部": "手部",
    "手掌": "手掌", "手指": "手指",
    "手臂": "手臂", "胳膊": "手臂", "臂部": "手臂",
    "肘": "肘部", "肘部": "肘部", "手肘": "肘部",
    "手腕": "手腕", "腕部": "手腕",
    "臀": "臀部", "臀部": "臀部",
    "腿": "腿部", "腿部": "腿部",
    "大腿": "大腿", "膝": "膝部", "膝部": "膝部", "膝盖": "膝部",
    "小腿": "小腿", "脚踝": "脚踝", "踝部": "脚踝",
    "脚": "脚部", "脚步": "脚部", "脚部": "脚部", "足部": "脚部",
    "脚底": "脚底", "足底": "脚底", "脚趾": "脚趾", "足趾": "脚趾",
    "全身": "全身",
}
DESCRIPTION_CLOSEUPS = tuple(
    (phrase, part)
    for alias, part in BODY_PART_TAGS.items()
    for phrase in (alias + "特写", alias + "的特写", "特写" + alias)
)
SPECIFIC_OBJECTS = (
    "紧身衣", "牛仔裤", "连衣裙", "短裙", "长裙", "短裤", "长裤", "运动裤", "丝袜", "短袜", "长袜",
    "运动鞋", "高跟鞋", "皮鞋", "凉鞋", "拖鞋", "泳装", "运动服", "西装", "外套", "上衣", "衬衫", "毛衣",
)
INVALID_TAGS = {"未知", "无法判断", "不确定", "其他", "内容", "画面", "媒体"}
GENERIC_CLOTHING = {"衣服", "服装", "衣物", "裤子", "鞋子"}

CANONICAL_ALIASES = {
    ("action", "posture"): {
        "坐着": "坐姿", "坐下": "坐姿", "坐姿": "坐姿",
        "躺着": "躺姿", "躺下": "躺姿", "躺姿": "躺姿",
        "站着": "站立", "站姿": "站立", "站立": "站立",
        "蹲着": "蹲姿", "蹲下": "蹲姿", "蹲姿": "蹲姿",
        "跪着": "跪姿", "跪姿": "跪姿",
        "趴着": "俯卧", "俯卧": "俯卧", "仰卧": "仰卧",
    },
    ("action", "activity"): {
        "走路": "行走", "步行": "行走", "行走": "行走",
        "舞蹈": "跳舞", "跳舞": "跳舞",
        "体操": "做操", "做操": "做操",
        "骑车": "骑行", "骑自行车": "骑行", "骑行": "骑行",
    },
    ("scene", "indoor"): {
        "室内阳台": "阳台", "阳台上": "阳台", "阳台": "阳台",
        "窗户边": "窗边", "窗前": "窗边", "窗边": "窗边",
        "商店": "商场", "商店内部": "商场", "商场": "商场",
        "房间": "室内", "室内环境": "室内", "室内": "室内",
    },
    ("scene", "outdoor"): {
        "户外": "室外", "室外环境": "室外", "室外": "室外",
        "沙滩": "海滩", "海滩": "海滩",
        "海岸": "海边", "海边": "海边",
    },
}

COLOR_ALIASES = {
    "黑": "黑", "黑色": "黑", "纯黑": "黑", "纯黑色": "黑", "浅黑": "黑", "深黑": "黑",
    "白": "白", "白色": "白", "纯白": "白", "纯白色": "白", "米白": "白", "米白色": "白",
    "乳白": "白", "乳白色": "白", "象牙白": "白", "象牙白色": "白", "米色": "白",
    "灰": "灰", "灰色": "灰", "浅灰": "灰", "浅灰色": "灰", "深灰": "灰", "深灰色": "灰",
    "银": "灰", "银色": "灰",
    "红": "红", "红色": "红", "浅红": "红", "浅红色": "红", "深红": "红", "深红色": "红", "酒红": "红", "酒红色": "红",
    "橙": "橙", "橙色": "橙", "浅橙": "橙", "浅橙色": "橙", "深橙": "橙", "深橙色": "橙",
    "黄": "黄", "黄色": "黄", "浅黄": "黄", "浅黄色": "黄", "深黄": "黄", "深黄色": "黄", "金": "黄", "金色": "黄",
    "绿": "绿", "绿色": "绿", "浅绿": "绿", "浅绿色": "绿", "深绿": "绿", "深绿色": "绿",
    "青": "青", "青色": "青", "青绿": "青", "青绿色": "青", "湖蓝": "青", "湖蓝色": "青",
    "蓝": "蓝", "蓝色": "蓝", "浅蓝": "蓝", "浅蓝色": "蓝", "深蓝": "蓝", "深蓝色": "蓝",
    "紫": "紫", "紫色": "紫", "浅紫": "紫", "浅紫色": "紫", "深紫": "紫", "深紫色": "紫",
    "粉": "粉", "粉色": "粉", "浅粉": "粉", "浅粉色": "粉", "深粉": "粉", "深粉色": "粉",
    "棕": "棕", "棕色": "棕", "浅棕": "棕", "浅棕色": "棕", "深棕": "棕", "深棕色": "棕",
    "褐": "棕", "褐色": "棕", "卡其": "棕", "卡其色": "棕",
}
VAGUE_COLORS = {"浅色", "深色", "亮色", "暗色", "淡色", "深浅色"}

STYLE_ALIASES = {
    "圆点": "波点", "圆点图案": "波点", "波点图案": "波点",
    "条纹图案": "条纹", "格子": "格纹", "格子图案": "格纹",
    "印花图案": "印花", "厚底款": "厚底", "高跟款": "高跟",
    "系带款": "系带", "绑带": "系带",
}

SHOE_TYPE_ALIASES = {
    "高跟凉拖": "高跟凉鞋", "凉拖": "拖鞋", "运动鞋子": "运动鞋",
    "短筒靴": "短靴", "长筒靴": "长靴",
}

SOCK_TYPE_ALIASES = {
    "短筒袜": "短袜", "中筒袜": "长袜", "长筒丝袜": "长筒袜",
    "过膝长袜": "过膝袜", "网眼袜": "网袜",
}

SOCK_TERMS = (
    "紧身裤袜", "连裤袜", "裤袜", "长筒丝袜", "丝袜", "过膝袜",
    "长筒袜", "短袜", "长袜", "船袜", "棉袜", "网袜", "网眼袜",
)

CHINESE_DIGITS = {"零": 0, "一": 1, "二": 2, "两": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9}

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

SCENE_HINTS = {
    "客厅", "卧室", "厨房", "浴室", "餐厅", "阳台", "走廊", "楼梯", "室内", "室外", "办公室", "会议室",
    "车间", "仓库", "体育馆", "游泳池", "操场", "舞台", "展厅", "商场", "商店内部", "海边", "海滩",
    "街道", "公园", "车内", "教室", "酒店",
}
ACTION_HINTS = {
    "坐着", "躺着", "站立", "站着", "俯卧", "仰卧", "跳舞", "做操", "走路", "跑步", "游泳", "骑行",
    "瑜伽", "健身", "登山", "徒步", "拍摄", "阅读", "写字", "烹饪", "工作", "学习",
}


def _chinese_integer(value):
    if not value:
        return None
    if all(char in CHINESE_DIGITS for char in value):
        digits = "".join(str(CHINESE_DIGITS[char]) for char in value)
        return int(digits)
    total = 0
    current = 0
    for char in value:
        if char in CHINESE_DIGITS:
            current = CHINESE_DIGITS[char]
        elif char == "十":
            total += (current or 1) * 10
            current = 0
        elif char == "百":
            total += (current or 1) * 100
            current = 0
        else:
            return None
    return total + current


def _canonical_people_count(*values):
    for value in values:
        compact = re.sub(r"\s+", "", str(value or ""))
        match = re.search(r"(\d{1,3})(?:个|名)?人", compact)
        if match:
            count = int(match.group(1))
            return f"{count}人" if count > 0 else ""
        match = re.search(r"([零一二两三四五六七八九十百]+)(?:个|名)?人", compact)
        if match:
            count = _chinese_integer(match.group(1))
            return f"{count}人" if count and count > 0 else ""
        if compact in {"单人", "独自一人"}:
            return "1人"
    return ""


def _canonical_value(value, aliases):
    value = re.sub(r"\s+", "", str(value or "")).strip("，,。；;：:")
    return aliases.get(value, value)


def _canonical_color(value):
    value = _canonical_value(value, {})
    if value in VAGUE_COLORS:
        return ""
    canonical = COLOR_ALIASES.get(value)
    if canonical:
        return canonical
    compact = re.sub(r"^(?:很|偏|浅|深|淡|亮|暗)+", "", value)
    return COLOR_ALIASES.get(compact, "")


def _normalize_label_color(label, raw_color, color):
    raw_color = _canonical_value(raw_color, {})
    if not raw_color:
        return label
    display = color + "色" if color else ""
    variants = {raw_color}
    if len(raw_color) == 1:
        variants.add(raw_color + "色")
    for variant in sorted(variants, key=len, reverse=True):
        if variant in label:
            return label.replace(variant, display, 1)
    return label


def _is_color_only(value):
    value = _canonical_value(value, {})
    return value in COLOR_ALIASES or value in VAGUE_COLORS


def _looks_like_socks(*values):
    text = " ".join(str(value or "") for value in values)
    return any(term in text for term in SOCK_TERMS) or "袜" in text


def _normalize_structured_item(item):
    item = dict(item)
    category = str(item.get("category", "")).strip()
    subject = str(item.get("subject", "")).strip()
    if category not in {"people", "action", "clothing", "closeup"}:
        return None
    label = _canonical_value(item.get("label"), {})
    item_type = _canonical_value(item.get("type"), {})
    raw_color = _canonical_value(item.get("color"), {})
    color = _canonical_color(raw_color)
    label = _normalize_label_color(label, raw_color, color)
    style = _canonical_value(item.get("style"), STYLE_ALIASES)

    visible_part = label.removesuffix("特写")
    if visible_part in BODY_PART_TAGS:
        normalized_part = BODY_PART_TAGS[visible_part]
        return {
            **item,
            "label": normalized_part + "特写",
            "category": "closeup",
            "subject": "part",
            "type": normalized_part,
            "color": "",
            "style": "",
        }

    if category == "people" and subject == "count":
        count = _canonical_people_count(item_type, label)
        if not count:
            return None
        return {**item, "label": count, "type": count, "color": "", "style": ""}

    aliases = CANONICAL_ALIASES.get((category, subject), {})
    if aliases:
        item_type = _canonical_value(item_type or label, aliases)
        label = item_type

    if _looks_like_socks(label, item_type):
        category = "clothing"
        subject = "socks"
        item_type = _canonical_value(item_type, SOCK_TYPE_ALIASES)
    elif category == "clothing" and subject == "shoes":
        item_type = _canonical_value(item_type, SHOE_TYPE_ALIASES)
    elif category == "clothing" and subject == "socks":
        item_type = _canonical_value(item_type, SOCK_TYPE_ALIASES)

    if category == "closeup" and subject == "part":
        part = (item_type or label).removesuffix("特写")
        normalized_part = BODY_PART_TAGS.get(part)
        if not normalized_part:
            return None
        label = normalized_part + "特写"
        item_type = normalized_part
        color = ""
        style = ""

    return {
        **item,
        "label": label,
        "category": category,
        "subject": subject,
        "type": item_type,
        "color": color,
        "style": style,
    }


def limit_closeup_candidates(candidates, hierarchy, media_type):
    maximum = 1 if media_type == "image" else 2
    result = []
    closeups = 0
    for tag in candidates:
        metadata = hierarchy.get(tag, {})
        if metadata.get("categoryKey") == "closeup":
            if closeups >= maximum:
                continue
            closeups += 1
        result.append(tag)
    return result, {tag: hierarchy[tag] for tag in result if tag in hierarchy}


def reconcile_closeups_from_description(description, candidates, hierarchy):
    matches = []
    for phrase, part in DESCRIPTION_CLOSEUPS:
        start = description.find(phrase)
        if start >= 0:
            matches.append((start, part))
    if not matches:
        return candidates, hierarchy

    additions = []
    seen_parts = set()
    for _, part in sorted(matches):
        if part in seen_parts:
            continue
        seen_parts.add(part)
        tag = part + "特写"
        if tag in candidates:
            continue
        metadata = metadata_from_model_tag({
            "label": tag,
            "category": "closeup",
            "subject": "part",
            "type": part,
            "color": "",
            "style": "",
        })
        if metadata is not None:
            additions.append(tag)
            hierarchy[tag] = metadata
    if not additions:
        return candidates, hierarchy
    merged = (additions + list(candidates))[:10]
    return merged, {tag: hierarchy[tag] for tag in merged if tag in hierarchy}


def parse_model_output(text, allowed_labels):
    description, candidates, _ = parse_model_analysis(text, allowed_labels)
    return description, candidates


def parse_model_analysis(text, allowed_labels, require_categories=False):
    payload = _decode_payload(text)
    if any(isinstance(item, dict) for item in payload.get("tags", [])):
        description, candidates, required, _ = _structured_payload(payload)
        return description, candidates, required
    return _legacy_payload(payload)


def parse_structured_model_analysis(text):
    payload = _decode_payload(text)
    return _structured_payload(payload)


def _decode_payload(text):
    value = text.strip()
    start = value.find("{")
    end = value.rfind("}")
    if start < 0 or end <= start:
        raise ValueError("model response does not contain a JSON object")
    return json.loads(value[start:end + 1])


def _legacy_payload(payload):
    description = str(payload.get("description", "")).strip().strip('“”"')
    required_tags = _required_tags(payload.get("requiredTags"))
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


def _structured_payload(payload):
    description = str(payload.get("description", "")).strip().strip('“”"')
    raw_tags = payload.get("tags", [])
    if not isinstance(raw_tags, list):
        raise ValueError("model tags must be an array")
    candidates = []
    hierarchy = {}
    for item in raw_tags:
        if not isinstance(item, dict):
            raise ValueError("structured model tags must be objects")
        item = _normalize_structured_item(item)
        if item is None:
            continue
        tag = str(item.get("label", "")).strip()
        if item.get("category") == "people" and item.get("subject") != "count":
            continue
        metadata = metadata_from_model_tag(item)
        if not _valid_tag(tag) or _is_color_only(tag) or metadata is None or tag in candidates:
            continue
        candidates.append(tag)
        hierarchy[tag] = metadata
        if len(candidates) == 10:
            break
    return description, candidates, set(candidates), hierarchy


def _required_tags(value):
    if not isinstance(value, dict):
        value = {}
    normalized = {}
    keys = ("shoes", "socks", "closeup", "clothing", "scenes", "actions", "people")
    for key in keys:
        items = value.get(key, [])
        if not isinstance(items, list):
            items = [items]
        normalized[key] = [str(item).strip() for item in items if _valid_tag(str(item).strip())]
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
    candidates = [tag for tag in candidates if "无法判断" not in tag]
    return candidates[:10], {tag for tag in required_tags if "无法判断" not in tag}


def _valid_tag(tag):
    if "无法判断" in tag or tag in INVALID_TAGS or len(tag) < 1 or len(tag) > 12:
        return False
    return re.fullmatch(r"[\u3400-\u9fffA-Za-z0-9＋+#·-]+", tag) is not None


def _color_object_phrase(description, object_name):
    for color in COLORS:
        color_name = color + "色"
        pattern = re.escape(color_name) + r"(?:的)?" + re.escape(object_name)
        if re.search(pattern, description):
            return color_name + object_name
    return ""


def _suppress_generic_tags(tags):
    unique = [tag for tag in dict.fromkeys(tags) if _valid_tag(tag)]
    specific_clothing = [tag for tag in unique if any(name in tag for name in SPECIFIC_OBJECTS)]
    result = []
    for tag in unique:
        if _is_color_only(tag):
            canonical = _canonical_color(tag)
            if any(value != tag and value.startswith(canonical + "色") for value in unique):
                continue
        if tag in SPECIFIC_OBJECTS and any(value != tag and value.endswith(tag) for value in unique):
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
