"""检查语料的可复现性、锚点隔离和计数标记。"""
from pathlib import Path
import json
import tempfile
import unittest

import gen_corpus
from corpus_eval import percentile


class CorpusTests(unittest.TestCase):
    def test_reproducible_manual_anchor_and_marker_counts(self):
        with tempfile.TemporaryDirectory() as tmp:
            a, b, c = (Path(tmp) / name for name in ('a', 'b', 'c'))
            first = gen_corpus.generate(114, 23, a)
            second = gen_corpus.generate(114, 23, b)
            third = gen_corpus.generate(114, 24, c)
            self.assertEqual(first, second)
            self.assertNotEqual(first['content_sha256'], third['content_sha256'])
            self.assertEqual(first['keyword_expected_chunks'], {'CORPUSMARKER007': 16, 'CORPUSMARKER113': 1, 'CORPUSMARKERZERO': 0})
            for original in (gen_corpus.ROOT / 'examples/docs').rglob('*'):
                if original.is_file():
                    self.assertEqual(original.read_bytes(), (a / 'manual' / original.relative_to(gen_corpus.ROOT / 'examples/docs')).read_bytes())
            texts = [p.read_text() for p in (a / 'noise').rglob('*.md')]
            self.assertEqual(len(texts), 114)
            for marker, expected in first['keyword_expected_chunks'].items():
                self.assertEqual(sum(text.count(marker) for text in texts), expected)
            suite = json.loads((gen_corpus.ROOT / 'examples/eval-questions.json').read_text())
            for case in suite['cases']:
                for item in case['expected']:
                    self.assertFalse(any(item['anchor'] in text for text in texts))
            with self.assertRaises(FileExistsError):
                gen_corpus.generate(1, 1, a)

    def test_source_batches_stay_within_server_limit(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / 'corpus'
            manifest = gen_corpus.generate(4001, 5, root)
            counts = [len(list((root / source['path']).rglob('*.md'))) for source in manifest['sources']]
            self.assertEqual(counts, [7, 4000, 1])
            self.assertEqual(sum(counts), 4008)

    def test_quantiles_include_endpoints_and_interpolate(self):
        self.assertEqual(percentile([100, 0, 20, 10], .5), 15)
        self.assertAlmostEqual(percentile([100, 0, 20, 10], .95), 88)
        self.assertEqual(percentile([9], .95), 9)


if __name__ == '__main__':
    unittest.main()
