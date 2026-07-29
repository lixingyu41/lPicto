import re


CATEGORY_LABELS = {
    "people": "人物",
    "action": "动作",
    "shoes": "鞋子",
    "socks": "袜子",
    "clothes": "衣服",
    "closeup": "特写",
}

CATEGORY_SUBJECTS = {
    "scene": {"indoor": "室内", "outdoor": "室外"},
    "people": {"count": "人数", "person": "人物类型"},
    "action": {"posture": "姿态", "activity": "活动"},
    "clothing": {
        "shoes": "鞋子", "socks": "袜子", "top": "上衣", "outerwear": "外套",
        "dress": "裙装", "pants": "裤装", "sportswear": "运动服", "swimwear": "泳装",
        "hat": "帽子", "accessories": "配饰",
    },
    "closeup": {"part": "部位"},
    "object": {"object": "物品"},
    "animal": {"animal": "动物"},
    "nature": {"nature": "自然"},
    "transport": {"transport": "交通"},
    "food": {"food": "食物"},
    "weather": {"weather": "时间天气"},
    "media": {"media": "媒体形式"},
    "other": {"other": "其他"},
}

COLORS = ("黑", "白", "灰", "红", "橙", "黄", "绿", "青", "蓝", "紫", "粉", "棕")
STYLES = ("厚底", "粗跟", "细跟", "高跟", "平底", "系带", "露趾", "紧身", "宽松", "吊带", "条纹", "格纹", "印花", "牛仔", "长款", "短款", "无袖", "长袖", "短袖")
SHOE_TYPES = ("高跟凉鞋", "运动鞋", "高跟鞋", "皮鞋", "凉鞋", "拖鞋", "靴子", "短靴", "长靴", "帆布鞋", "板鞋")
SOCK_TYPES = ("紧身裤袜", "连裤袜", "裤袜", "过膝袜", "长筒袜", "丝袜", "短袜", "长袜", "船袜", "棉袜", "网袜")
CLOTHING_SUBJECTS = (
    ("外套", ("外套", "风衣", "大衣", "夹克")),
    ("裙装", ("连衣裙", "短裙", "长裙", "吊带裙", "半身裙", "裙子")),
    ("裤装", ("牛仔裤", "短裤", "长裤", "运动裤", "裤子")),
    ("上衣", ("紧身衣", "上衣", "衬衫", "毛衣", "背心", "吊带", "T恤")),
    ("运动服", ("运动服", "瑜伽服")),
    ("泳装", ("泳装", "泳衣", "比基尼")),
    ("帽子", ("帽子", "棒球帽")),
)
ACTION_POSTURES = ("坐着", "坐姿", "躺着", "躺姿", "站立", "站着", "俯卧", "仰卧", "蹲着", "跪着")
ACTION_ACTIVITIES = ("行走", "走路", "跳舞", "做操", "跑步", "游泳", "骑行", "瑜伽", "健身", "登山", "徒步", "拍摄", "阅读", "写字", "烹饪", "工作", "学习", "挥手", "比心", "微笑", "摆姿势")
CLOSEUP_PARTS = (
    "脸部", "头部", "眼部", "鼻部", "嘴部", "嘴唇", "舌部", "牙齿", "耳部",
    "颈部", "肩部", "锁骨", "胸部", "腹部", "肚脐", "腰部", "背部",
    "手部", "手掌", "手指", "手臂", "肘部", "手腕",
    "臀部", "腿部", "大腿", "膝部", "小腿", "脚踝", "脚部", "脚底", "脚趾", "全身",
)
INDOOR_SCENES = ("室内", "客厅", "卧室", "厨房", "浴室", "餐厅", "阳台", "走廊", "办公室", "会议室", "车间", "仓库", "体育馆", "舞台", "展厅", "商场", "教室", "酒店", "窗边")
OUTDOOR_SCENES = ("室外", "海边", "海滩", "街道", "公园", "操场", "城市", "山脉", "森林", "草地", "花园")
ANIMALS = ("猫", "狗", "鸟", "鱼", "兔子", "马", "牛", "羊", "熊猫", "蝴蝶")
NATURE = ("天空", "云朵", "日出", "日落", "星空", "山脉", "森林", "草地", "花朵", "海滩", "海洋", "湖泊", "河流", "瀑布")
TRANSPORT = ("汽车", "卡车", "公交车", "出租车", "摩托车", "自行车", "火车", "地铁", "飞机", "轮船")
FOOD = ("食物", "米饭", "面条", "面包", "蛋糕", "水果", "苹果", "香蕉", "蔬菜", "咖啡", "茶")
WEATHER = ("白天", "夜晚", "晴天", "阴天", "雨天", "雪天", "春天", "夏天", "秋天", "冬天")
MEDIA = ("自拍", "合影", "航拍", "截图", "动画", "插画", "黑白照片", "老照片")


def _node_id(parts):
    return "ai:" + ".".join(parts)


def _facet(category_key, category_label, subject_key, subject_label, dimension_key, dimension_label, value):
    keys = [category_key, subject_key, dimension_key]
    labels = [category_label, subject_label, dimension_label]
    node_ids = [_node_id(keys[: index + 1]) for index in range(len(keys))]
    value_id = node_ids[-1] + ":" + value
    return {
        "facetKey": ".".join(keys),
        "nodeId": value_id,
        "nodeIds": node_ids + [value_id],
        "labels": labels + [value],
    }


def _metadata(tag, category_key, subject_key, subject_label, facets):
    return {
        "categoryKey": category_key,
        "categoryLabel": CATEGORY_LABELS[category_key],
        "subjectKey": subject_key,
        "subjectLabel": subject_label,
        "facets": facets,
    }


def _flat_facet(category_key, category_label, dimension_key, dimension_label, value):
    keys = [category_key, dimension_key]
    labels = [category_label, dimension_label]
    node_ids = [_node_id(keys[: index + 1]) for index in range(len(keys))]
    value_id = node_ids[-1] + ":" + value
    return {
        "facetKey": ".".join(keys),
        "nodeId": value_id,
        "nodeIds": node_ids + [value_id],
        "labels": labels + [value],
    }


def _split_clothing_metadata(tag, item):
    subject_key = str(item.get("subject", "")).strip()
    dimensions = [("type", "类型")]
    if subject_key in {"shoes", "socks"}:
        category_key = subject_key
        category_label = CATEGORY_LABELS[category_key]
        dimensions.extend((("color", "颜色"), ("style", "款式")))
        facets = []
        for dimension_key, dimension_label in dimensions:
            value = str(item.get(dimension_key, "")).strip()
            if value and "无法判断" not in value:
                facets.append(_flat_facet(category_key, category_label, dimension_key, dimension_label, value))
        if not facets:
            facets.append(_flat_facet(category_key, category_label, "type", "类型", tag))
        return {
            "categoryKey": category_key,
            "categoryLabel": category_label,
            "subjectKey": category_key,
            "subjectLabel": category_label,
            "facets": facets,
        }

    subject_label = CATEGORY_SUBJECTS["clothing"].get(subject_key)
    if not subject_label:
        return None
    facets = []
    dimensions.extend((("color", "颜色"), ("style", "款式")))
    for dimension_key, dimension_label in dimensions:
        value = str(item.get(dimension_key, "")).strip()
        if value and "无法判断" not in value:
            facets.append(_facet(
                "clothes", CATEGORY_LABELS["clothes"], subject_key, subject_label,
                dimension_key, dimension_label, value,
            ))
    if not facets:
        facets.append(_facet(
            "clothes", CATEGORY_LABELS["clothes"], subject_key, subject_label,
            "type", "类型", tag,
        ))
    return _metadata(tag, "clothes", subject_key, subject_label, facets)


def metadata_from_model_tag(item):
    """Build hierarchy only from the category emitted by the vision model."""
    if not isinstance(item, dict):
        return None
    tag = str(item.get("label", "")).strip()
    category_key = str(item.get("category", "")).strip()
    subject_key = str(item.get("subject", "")).strip()
    if not tag or "无法判断" in tag or category_key not in CATEGORY_SUBJECTS:
        return None
    if category_key == "clothing":
        return _split_clothing_metadata(tag, item)
    subject_label = CATEGORY_SUBJECTS[category_key].get(subject_key)
    if not subject_label:
        return None
    facets = []
    dimensions = [("type", "类型")]
    if category_key == "clothing":
        dimensions.extend((("color", "颜色"), ("style", "款式")))
    for dimension_key, dimension_label in dimensions:
        value = str(item.get(dimension_key, "")).strip()
        if value and "无法判断" not in value:
            facets.append(_facet(
                category_key, CATEGORY_LABELS[category_key], subject_key, subject_label,
                dimension_key, dimension_label, value,
            ))
    if not facets:
        facets.append(_facet(
            category_key, CATEGORY_LABELS[category_key], subject_key, subject_label,
            "type", "类型", tag,
        ))
    return _metadata(tag, category_key, subject_key, subject_label, facets)


def _first_match(tag, values):
    return next((value for value in values if value in tag), "")


def _color_from_tag(tag):
    aliases = (
        ("米白", "白"), ("乳白", "白"), ("象牙白", "白"), ("米色", "白"),
        ("银", "灰"), ("金", "黄"), ("褐", "棕"), ("卡其", "棕"),
    )
    for source, target in aliases:
        if source in tag:
            return target
    return _first_match(tag, COLORS)


def _clothing_metadata(tag, subject_key, subject_label, type_values):
    category_key = subject_key if subject_key in {"shoes", "socks"} else "clothes"
    category_label = CATEGORY_LABELS[category_key]
    facets = []
    item_type = _first_match(tag, type_values)
    color = _color_from_tag(tag)
    styles = [style for style in STYLES if style in tag and style != item_type]
    if item_type:
        facet = _flat_facet(category_key, category_label, "type", "类型", item_type) if category_key in {"shoes", "socks"} else _facet(category_key, category_label, subject_key, subject_label, "type", "类型", item_type)
        facets.append(facet)
    if color:
        facet = _flat_facet(category_key, category_label, "color", "颜色", color) if category_key in {"shoes", "socks"} else _facet(category_key, category_label, subject_key, subject_label, "color", "颜色", color)
        facets.append(facet)
    for style in styles[:2]:
        facet = _flat_facet(category_key, category_label, "style", "款式", style) if category_key in {"shoes", "socks"} else _facet(category_key, category_label, subject_key, subject_label, "style", "款式", style)
        facets.append(facet)
    if not facets:
        facet = _flat_facet(category_key, category_label, "state", "状态", tag) if category_key in {"shoes", "socks"} else _facet(category_key, category_label, subject_key, subject_label, "state", "状态", tag)
        facets.append(facet)
    return _metadata(tag, category_key, subject_key, subject_label, facets)


def classify_tag(tag):
    tag = str(tag).strip()
    if "无法判断" in tag:
        return None
    if tag == "未穿鞋" or any(value in tag for value in SHOE_TYPES) or tag.endswith("鞋"):
        return _clothing_metadata(tag, "shoes", "鞋子", SHOE_TYPES)
    if tag == "未穿袜" or "袜" in tag:
        return _clothing_metadata(tag, "socks", "袜子", SOCK_TYPES)
    for subject_label, values in CLOTHING_SUBJECTS:
        if any(value in tag for value in values):
            key = {
                "外套": "outerwear", "裙装": "dress", "裤装": "pants", "上衣": "top",
                "运动服": "sportswear", "泳装": "swimwear", "帽子": "hat",
            }[subject_label]
            return _clothing_metadata(tag, key, subject_label, values)
    if tag == "无明显特写" or tag.endswith("特写"):
        part = _first_match(tag, CLOSEUP_PARTS) or tag.removesuffix("特写")
        facet = _facet("closeup", CATEGORY_LABELS["closeup"], "part", "部位", "part", "部位", part or "无明显")
        return _metadata(tag, "closeup", "part", "部位", [facet])
    if re.fullmatch(r"(?:一个人|两个人|\d+个人|\d+人)", tag):
        facet = _facet("people", CATEGORY_LABELS["people"], "count", "人数", "count", "人数", tag)
        return _metadata(tag, "people", "count", "人数", [facet])
    if tag in ACTION_POSTURES:
        facet = _facet("action", CATEGORY_LABELS["action"], "posture", "姿态", "posture", "姿态", tag)
        return _metadata(tag, "action", "posture", "姿态", [facet])
    if tag in ACTION_ACTIVITIES or any(value in tag for value in ACTION_ACTIVITIES):
        facet = _facet("action", CATEGORY_LABELS["action"], "activity", "活动", "activity", "活动", tag)
        return _metadata(tag, "action", "activity", "活动", [facet])
    return None


def attach_tag_hierarchy(tags, model_metadata=None):
    result = []
    model_metadata = model_metadata or {}
    for item in tags:
        metadata = model_metadata.get(item.get("tag", "")) or classify_tag(item.get("tag", ""))
        if metadata is None:
            continue
        result.append({**item, **metadata})
    return result[:10]
