import unittest
from media_sampling import sample_ratios

class SamplingTest(unittest.TestCase):
    def test_video_boundaries(self):
        self.assertEqual(sample_ratios(9.99), [0.25, 0.5, 0.75])
        self.assertEqual(sample_ratios(10), [0.1, 0.5, 0.9])
        self.assertEqual(sample_ratios(60), [0.1, 0.5, 0.9])
        self.assertEqual(sample_ratios(60.01), [0.05, 0.2, 0.35, 0.5, 0.65, 0.8, 0.95])
        self.assertEqual(len(sample_ratios(300)), 7)
        self.assertEqual(len(sample_ratios(300.01)), 9)
        self.assertEqual(len(sample_ratios(1200)), 9)
        self.assertEqual(len(sample_ratios(1200.01)), 10)

if __name__ == "__main__":
    unittest.main()
