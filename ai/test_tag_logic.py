import unittest

import numpy as np

from tag_logic import (
    augment_candidates_from_description,
    parse_model_analysis,
    parse_model_output,
    reconcile_required_defaults,
    select_validated_tags,
)


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

    def test_required_classification_fills_every_missing_category(self):
        _, candidates, required = parse_model_analysis(
            '{"description":"画面细节不足。","requiredTags":{},"tags":[]}',
            (),
            require_categories=True,
        )
        self.assertEqual(
            candidates,
            ["鞋子无法判断", "袜子无法判断", "无明显特写", "服装无法判断", "场景无法判断", "动作无法判断", "人数无法判断"],
        )
        self.assertEqual(required, set(candidates))

    def test_concrete_tags_replace_conflicting_unknown_defaults(self):
        candidates, required = reconcile_required_defaults(
            ["未穿鞋", "未穿袜", "脚部特写", "白色短裤", "场景无法判断", "俯卧", "一个人", "室内", "卧室"],
            {"未穿鞋", "未穿袜", "脚部特写", "白色短裤", "场景无法判断", "俯卧", "一个人"},
        )
        self.assertNotIn("场景无法判断", candidates)
        self.assertIn("室内", required)


if __name__ == "__main__":
    unittest.main()
