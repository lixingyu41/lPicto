import unittest

import numpy as np

from tag_logic import augment_candidates_from_description, parse_model_output, select_validated_tags


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

    def test_description_adds_exact_and_synonym_candidates(self):
        tags = augment_candidates_from_description(
            "粉色拖鞋放在室内地板上。",
            ["室内"],
            ("粉色", "鞋子", "室内", "人物"),
        )
        self.assertEqual(tags, ["室内", "粉色", "鞋子"])


if __name__ == "__main__":
    unittest.main()
