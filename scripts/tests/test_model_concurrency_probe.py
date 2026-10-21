import importlib.util
from pathlib import Path
import sys
import unittest

path = Path(__file__).resolve().parents[1] / 'model_concurrency_probe.py'
spec = importlib.util.spec_from_file_location('model_concurrency_probe', path)
probe = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = probe
spec.loader.exec_module(probe)


class ProbeTests(unittest.TestCase):
    def rows(self, n):
        return [dict(ok=True, seconds=10, first_token_seconds=1) for _ in range(n)]

    def test_capacity_rejection_cannot_be_hidden_by_success_rate(self):
        rows = self.rows(20)
        rows[-1].update(ok=False, error_kind='capacity_rejection')
        self.assertFalse(probe.assess(rows, 10, 10)['passed'])

    def test_requires_real_target_overlap(self):
        self.assertFalse(probe.assess(self.rows(20), 10, 8)['passed'])
        self.assertTrue(probe.assess(self.rows(20), 10, 10)['passed'])

    def test_invalid_output_is_not_a_success(self):
        rows = self.rows(20)
        rows[-1].update(ok=False, error_kind='invalid_output')
        result = probe.assess(rows, 10, 10)
        self.assertEqual(result['success_rate'], 0.95)
        self.assertEqual(result['success'], 19)

    def test_queue_saturation_uses_latency_and_throughput(self):
        old = dict(output_chars_per_second=100, p95_seconds=10)
        self.assertTrue(probe.saturated(old, dict(output_chars_per_second=105, p95_seconds=20)))
        self.assertFalse(probe.saturated(old, dict(output_chars_per_second=190, p95_seconds=20)))

    def test_error_classification(self):
        self.assertEqual(probe.classify_error('llm http 429'), 'capacity_rejection')
        self.assertEqual(probe.classify_error('concurrency limit exceeded'), 'capacity_rejection')
        self.assertEqual(probe.classify_error('relay stream error event'), 'stream_failure')
        self.assertEqual(probe.classify_error('invalid llm response'), 'invalid_output')


if __name__ == '__main__':
    unittest.main()
