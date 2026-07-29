import unittest

import numpy as np

from tag_logic import (
    augment_candidates_from_description,
    limit_closeup_candidates,
    parse_model_analysis,
    parse_model_output,
    parse_structured_model_analysis,
    reconcile_closeups_from_description,
    reconcile_required_defaults,
    select_validated_tags,
)
from tag_hierarchy import attach_tag_hierarchy, classify_tag


class TagLogicTest(unittest.TestCase):
    def test_parse_filters_unknown_and_duplicate_tags(self):
        description, tags = parse_model_output(
            '```json\n{"description":"画面中有一只猫。","tags":["猫","未知","猫","室内"]}\n```',
            ("猫", "室内"),
        )
        self.assertEqual(description, "画面中有一只猫。")
        self.assertEqual(tags, ["猫", "室内"])

    def test_video_requires_two_matching_frames_and_limits_tags(self):
        labels = {"猫": 0, "室内": 1, "人物": 2}
        scores = np.array([[0.31, 0.40, 0.29], [0.32, 0.10, 0.30], [0.10, 0.11, 0.20]])
        tags = select_validated_tags(scores, ["猫", "室内", "人物"], labels, "video")
        self.assertEqual([item["tag"] for item in tags], ["猫", "人物"])

    def test_description_builds_color_object_compound(self):
        tags = augment_candidates_from_description(
            "粉色拖鞋放在室内地板上。",
            ["室内"],
            ("粉色", "鞋子", "室内", "人物"),
        )
        self.assertEqual(tags, ["室内", "粉色拖鞋"])

    def test_specific_clothing_suppresses_color_and_generic_tags(self):
        tags = augment_candidates_from_description(
            "一位穿着紫色紧身衣和牛仔裤的女子正在跳舞。",
            ["紫色", "衣服", "裤子", "人物", "紫色紧身衣", "牛仔裤", "跳舞"],
            ("紫色", "衣服", "裤子", "人物", "舞蹈"),
        )
        self.assertEqual(tags, ["紫色紧身衣", "牛仔裤", "跳舞"])

    def test_required_classification_tags_are_preserved_below_clip_threshold(self):
        description, candidates, required = parse_model_analysis(
            '{"description":"一位穿紫色上衣的人站在室内。","requiredTags":{"shoes":"未穿鞋","socks":"白色短袜","closeup":"脚部特写","clothing":["紫色上衣"],"scenes":["室内"],"actions":["站立"],"people":"一个人"},"tags":[]}',
            (),
        )
        self.assertEqual(description, "一位穿紫色上衣的人站在室内。")
        self.assertEqual(candidates, ["未穿鞋", "白色短袜", "脚部特写", "紫色上衣", "室内", "站立", "一个人"])
        scores = np.full((3, len(candidates)), 0.05, dtype=np.float32)
        tags = select_validated_tags(
            scores,
            candidates,
            {tag: index for index, tag in enumerate(candidates)},
            "video",
            required_tags=required,
        )
        self.assertEqual({item["tag"] for item in tags}, set(candidates))

    def test_required_classification_does_not_create_unknown_tags(self):
        _, candidates, required = parse_model_analysis(
            '{"description":"画面细节不足。","requiredTags":{},"tags":[]}',
            (),
            require_categories=True,
        )
        self.assertEqual(candidates, [])
        self.assertEqual(required, set())

    def test_unknown_tags_are_removed(self):
        candidates, required = reconcile_required_defaults(
            ["未穿鞋", "未穿袜", "脚部特写", "白色短裤", "场景无法判断", "俯卧", "一个人", "室内", "卧室"],
            {"未穿鞋", "未穿袜", "脚部特写", "白色短裤", "场景无法判断", "俯卧", "一个人"},
        )
        self.assertNotIn("场景无法判断", candidates)
        self.assertNotIn("场景无法判断", required)

    def test_hierarchical_shoe_facets_do_not_consume_tag_slots(self):
        metadata = classify_tag("白色厚底凉鞋")
        self.assertEqual(metadata["categoryLabel"], "鞋子")
        self.assertEqual(metadata["subjectLabel"], "鞋子")
        self.assertEqual(
            {facet["facetKey"] for facet in metadata["facets"]},
            {"shoes.type", "shoes.color", "shoes.style"},
        )
        tags = attach_tag_hierarchy([
            {"tag": "白色厚底凉鞋", "confidence": 0.8},
            {"tag": "鞋子无法判断", "confidence": 0.2},
        ])
        self.assertEqual(len(tags), 1)
        self.assertEqual(tags[0]["tag"], "白色厚底凉鞋")

    def test_model_categories_drive_hierarchy_and_orphan_colors_are_removed(self):
        description, candidates, required, hierarchy = parse_structured_model_analysis(
            '{"description":"一位女孩在阳台勾脚，穿白色波点纱裙。","tags":['
            '{"label":"白色波点纱裙","category":"clothing","subject":"dress","type":"纱裙","color":"白","style":"波点"},'
            '{"label":"阳台","category":"scene","subject":"indoor","type":"阳台","color":"","style":""},'
            '{"label":"勾脚","category":"action","subject":"activity","type":"勾脚","color":"","style":""},'
            '{"label":"女孩","category":"people","subject":"person","type":"女孩","color":"","style":""},'
            '{"label":"脚步","category":"people","subject":"person","type":"脚步","color":"白","style":""},'
            '{"label":"蓝色","category":"object","subject":"object","type":"蓝色","color":"","style":""}'
            ']}'
        )
        self.assertIn("白色波点纱裙", description)
        self.assertEqual(candidates, ["白色波点纱裙", "勾脚", "脚部特写"])
        self.assertEqual(required, set(candidates))
        self.assertEqual(hierarchy["白色波点纱裙"]["categoryLabel"], "衣服")
        self.assertEqual(hierarchy["白色波点纱裙"]["subjectLabel"], "裙装")
        self.assertEqual(hierarchy["勾脚"]["categoryLabel"], "动作")
        self.assertEqual(hierarchy["脚部特写"]["categoryLabel"], "特写")

    def test_people_count_variants_share_one_canonical_node(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"两个视频中的画面都只有一个人。","tags":['
            '{"label":"一人","category":"people","subject":"count","type":"1人","color":"","style":""},'
            '{"label":"一个人","category":"people","subject":"count","type":"一人","color":"","style":""}'
            ']}'
        )
        self.assertEqual(candidates, ["1人"])
        facets = hierarchy["1人"]["facets"]
        self.assertEqual(facets[0]["nodeId"], "ai:people.count.type:1人")

    def test_bounded_synonyms_are_canonicalized_before_hierarchy(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"人物坐在室内阳台，穿纯白色圆点长裙。","tags":['
            '{"label":"坐着","category":"action","subject":"posture","type":"坐着","color":"","style":""},'
            '{"label":"室内阳台","category":"scene","subject":"indoor","type":"室内阳台","color":"","style":""},'
            '{"label":"纯白色圆点长裙","category":"clothing","subject":"dress","type":"长裙","color":"纯白色","style":"圆点图案"}'
            ']}'
        )
        self.assertEqual(candidates, ["坐姿", "白色圆点长裙"])
        clothing_facets = hierarchy["白色圆点长裙"]["facets"]
        values = {facet["facetKey"]: facet["labels"][-1] for facet in clothing_facets}
        self.assertEqual(values["clothes.dress.color"], "白")
        self.assertEqual(values["clothes.dress.style"], "波点")

    def test_tights_override_model_pants_classification(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"人物穿着浅灰色紧身裤袜。","tags":['
            '{"label":"浅灰色紧身裤袜","category":"clothing","subject":"pants","type":"紧身裤袜","color":"浅灰色","style":"紧身"}'
            ']}'
        )
        self.assertEqual(candidates, ["灰色紧身裤袜"])
        metadata = hierarchy["灰色紧身裤袜"]
        self.assertEqual(metadata["subjectKey"], "socks")
        values = {facet["facetKey"]: facet["labels"][-1] for facet in metadata["facets"]}
        self.assertEqual(values["socks.type"], "紧身裤袜")
        self.assertEqual(values["socks.color"], "灰")

    def test_plain_tight_pants_remain_pants(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"人物穿黑色紧身裤。","tags":['
            '{"label":"黑色紧身裤","category":"clothing","subject":"pants","type":"紧身裤","color":"黑","style":"紧身"}'
            ']}'
        )
        self.assertEqual(candidates, ["黑色紧身裤"])
        self.assertEqual(hierarchy["黑色紧身裤"]["subjectKey"], "pants")

    def test_fixed_colors_remove_depth_and_reject_vague_color(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"画面中有米白色上衣和浅色长裙。","tags":['
            '{"label":"米白色上衣","category":"clothing","subject":"top","type":"上衣","color":"米白色","style":""},'
            '{"label":"浅色长裙","category":"clothing","subject":"dress","type":"长裙","color":"浅色","style":""}'
            ']}'
        )
        self.assertEqual(candidates, ["白色上衣", "长裙"])
        top_values = {facet["facetKey"]: facet["labels"][-1] for facet in hierarchy["白色上衣"]["facets"]}
        self.assertEqual(top_values["clothes.top.color"], "白")
        dress_keys = {facet["facetKey"] for facet in hierarchy["长裙"]["facets"]}
        self.assertNotIn("clothes.dress.color", dress_keys)

    def test_clothing_is_split_into_parallel_roots(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"1人站立，穿运动鞋、短袜和上衣。","tags":['
            '{"label":"运动鞋","category":"clothing","subject":"shoes","type":"运动鞋","color":"","style":""},'
            '{"label":"短袜","category":"clothing","subject":"socks","type":"短袜","color":"","style":""},'
            '{"label":"上衣","category":"clothing","subject":"top","type":"上衣","color":"","style":""},'
            '{"label":"1人","category":"people","subject":"count","type":"1人","color":"","style":""},'
            '{"label":"站立","category":"action","subject":"posture","type":"站立","color":"","style":""}'
            ']}'
        )
        self.assertEqual(candidates, ["运动鞋", "短袜", "上衣", "1人", "站立"])
        self.assertEqual(
            [hierarchy[tag]["categoryLabel"] for tag in candidates],
            ["鞋子", "袜子", "衣服", "人物", "动作"],
        )

    def test_expanded_closeups_and_media_limits(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"视频依次展示手臂、胸部和腹部。","tags":['
            '{"label":"胳膊特写","category":"closeup","subject":"part","type":"胳膊","color":"","style":""},'
            '{"label":"胸部特写","category":"closeup","subject":"part","type":"胸部","color":"","style":""},'
            '{"label":"腹部特写","category":"closeup","subject":"part","type":"腹部","color":"","style":""}'
            ']}'
        )
        self.assertEqual(candidates, ["手臂特写", "胸部特写", "腹部特写"])
        image_candidates, _ = limit_closeup_candidates(candidates, hierarchy, "image")
        video_candidates, _ = limit_closeup_candidates(candidates, hierarchy, "video")
        self.assertEqual(image_candidates, ["手臂特写"])
        self.assertEqual(video_candidates, ["手臂特写", "胸部特写"])

    def test_description_closeup_is_restored_when_model_omits_tag(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"1人脚部特写，穿着白色绑带厚底凉鞋。","tags":['
            '{"label":"白色绑带厚底凉鞋","category":"clothing","subject":"shoes","type":"凉鞋","color":"白","style":"绑带厚底"}'
            ']}'
        )
        candidates, hierarchy = reconcile_closeups_from_description(
            "1人脚部特写，穿着白色绑带厚底凉鞋。",
            candidates,
            hierarchy,
        )
        self.assertEqual(candidates, ["脚部特写", "白色绑带厚底凉鞋"])
        self.assertEqual(hierarchy["脚部特写"]["categoryLabel"], "特写")

    def test_detailed_closeups_are_normalized_and_restored(self):
        _, candidates, _, hierarchy = parse_structured_model_analysis(
            '{"description":"1人，嘴部的特写，嘴唇涂有粉色唇彩。","tags":['
            '{"label":"1人","category":"people","subject":"count","type":"1人","color":"","style":""}'
            ']}'
        )
        candidates, hierarchy = reconcile_closeups_from_description(
            "1人，嘴部的特写，嘴唇涂有粉色唇彩。",
            candidates,
            hierarchy,
        )
        self.assertEqual(candidates, ["嘴部特写", "1人"])
        self.assertEqual(hierarchy["嘴部特写"]["categoryLabel"], "特写")

        _, reversed_candidates, _, reversed_hierarchy = parse_structured_model_analysis(
            '{"description":"画面特写脖子。","tags":[]}'
        )
        reversed_candidates, reversed_hierarchy = reconcile_closeups_from_description(
            "画面特写脖子。",
            reversed_candidates,
            reversed_hierarchy,
        )
        self.assertEqual(reversed_candidates, ["颈部特写"])
        self.assertEqual(reversed_hierarchy["颈部特写"]["categoryLabel"], "特写")

    def test_restored_closeup_keeps_ten_tag_limit(self):
        candidates = [f"衣物{index}" for index in range(10)]
        hierarchy = {
            tag: {"categoryKey": "clothes", "categoryLabel": "衣服", "facets": []}
            for tag in candidates
        }
        candidates, hierarchy = reconcile_closeups_from_description(
            "画面为手臂特写。",
            candidates,
            hierarchy,
        )
        self.assertEqual(len(candidates), 10)
        self.assertEqual(candidates[0], "手臂特写")
        self.assertNotIn("衣物9", candidates)


if __name__ == "__main__":
    unittest.main()
